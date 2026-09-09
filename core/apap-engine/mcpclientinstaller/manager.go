// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Package mcpclientinstaller discovers MCP clients and manages their MCP server configuration.
package mcpclientinstaller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Arm-Debug/apap-cli/apap-engine/mcpclientinstaller/clientids"
)

const maxCommandOutput = 64 * 1024
const clientCommandTimeout = 10 * time.Second

// CommandRunner invokes a client command without a shell and returns its
// combined, size-limited output.
type CommandRunner func(context.Context, string, ...string) ([]byte, error)

// ServerDefinition describes the MCP server entry supplied to every client.
type ServerDefinition struct {
	Name    string
	Command string
	Args    []string
}

// RegistrationState describes the client's current entry for the server name.
type RegistrationState uint8

const (
	RegistrationStateNotConfigured RegistrationState = iota + 1
	RegistrationStateConfigured
	RegistrationStateConflict
	RegistrationStateUnreadable
)

// RegistrationDifferenceField identifies one part of an existing MCP server
// entry that differs from the server definition managed by the installer.
type RegistrationDifferenceField uint8

const (
	RegistrationDifferenceCommand RegistrationDifferenceField = iota + 1
	RegistrationDifferenceArguments
	RegistrationDifferenceTransport
	RegistrationDifferenceEnvironment
)

// RegistrationDifference records the configured and expected values for one
// conflicting field. Environment values are deliberately never included.
type RegistrationDifference struct {
	Field    RegistrationDifferenceField
	Actual   string
	Expected string
}

// InstallOutcome describes whether installation changed client configuration.
type InstallOutcome uint8

const (
	InstallOutcomeInstalled InstallOutcome = iota + 1
	InstallOutcomeAlreadyConfigured
)

// UninstallOutcome describes whether removal changed client configuration.
type UninstallOutcome uint8

const (
	UninstallOutcomeRemoved UninstallOutcome = iota + 1
	UninstallOutcomeAlreadyAbsent
)

// ClientDiscovery describes the commands and paths inspected during automatic
// client discovery. It is returned for setup guidance, not used as a cache.
type ClientDiscovery struct {
	Commands []string
	Paths    []string
}

// ClientStatus is a point-in-time result. Detection results and resolved paths
// are deliberately not persisted.
type ClientStatus struct {
	ID                      string
	Detected                bool
	ExecutablePath          string
	ConfigurationPath       string
	State                   RegistrationState
	RegistrationDifferences []RegistrationDifference
	Err                     error
	DiscoveryCommands       []string
	DiscoveryPaths          []string
}

// InstallResult contains the install outcome and resulting client status.
type InstallResult struct {
	Outcome InstallOutcome
	Status  ClientStatus
}

// UninstallResult contains the uninstall outcome and resulting client status.
type UninstallResult struct {
	Outcome UninstallOutcome
	Status  ClientStatus
}

// ErrorKind classifies failures for translation at the Apap service boundary.
type ErrorKind uint8

const (
	ErrorUnknownClient ErrorKind = iota + 1
	ErrorClientUnavailable
	ErrorConfigRead
	ErrorConfigWrite
	ErrorConflict
	ErrorClientCommand
)

// InstallerError retains the client identity and underlying technical cause.
type InstallerError struct {
	Kind                    ErrorKind
	ClientID                string
	RegistrationDifferences []RegistrationDifference
	CommandError            string
	CommandOutput           string
	Err                     error
}

func (e *InstallerError) Error() string {
	clientName := clientids.DisplayName(e.ClientID)
	if e.Err != nil {
		return fmt.Sprintf("MCP client %s: %v", clientName, e.Err)
	}
	return fmt.Sprintf("MCP client %s: operation failed", clientName)
}
func (e *InstallerError) Unwrap() error { return e.Err }

type resolvedClient struct {
	detected          bool
	executablePath    string
	configurationPath string
}

// client is implemented once per supported MCP client. Detect must inspect the
// host on every call and must not cache its result. RegistrationState must
// inspect the configuration without modifying it. Install and Uninstall must
// only modify the named server entry and return the resulting registration
// state.
type client interface {
	Discovery() ClientDiscovery
	Detect(context.Context) (resolvedClient, error)
	RegistrationState(
		context.Context,
		ServerDefinition,
		resolvedClient,
	) (RegistrationState, []RegistrationDifference, error)
	Install(
		context.Context,
		ServerDefinition,
		resolvedClient,
	) (RegistrationState, []RegistrationDifference, error)
	Uninstall(
		context.Context,
		ServerDefinition,
		resolvedClient,
	) (RegistrationState, []RegistrationDifference, error)
}

// Config supplies platform values and injectable operating-system operations.
// The injected functions keep client detection and mutation deterministic in
// unit tests.
type Config struct {
	Server        ServerDefinition
	HomeDirectory string
	GOOS          string
	Getenv        func(string) string
	LookupPath    func(string) (string, error)
	RunCommand    CommandRunner
	Stat          func(string) (os.FileInfo, error)
}

type clientDependencies struct {
	home, goos string
	getenv     func(string) string
	lookupPath func(string) (string, error)
	run        CommandRunner
	stat       func(string) (os.FileInfo, error)
}

// Manager coordinates independent client implementations. It stores no client
// detection results or resolved paths.
type Manager struct {
	server  ServerDefinition
	clients map[string]client
}

// New creates a manager using the current user's host environment.
func New(server ServerDefinition) (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return NewWithConfig(Config{
		Server:        server,
		HomeDirectory: home,
		GOOS:          runtime.GOOS,
		Getenv:        os.Getenv,
		LookupPath:    exec.LookPath,
		RunCommand:    runCommand,
		Stat:          os.Stat,
	})
}

// NewWithConfig creates a manager with injectable host operations.
func NewWithConfig(config Config) (*Manager, error) {
	if config.Server.Name == "" {
		return nil, fmt.Errorf("MCP server name is empty")
	}
	if config.Server.Command == "" {
		return nil, fmt.Errorf("MCP server command is empty")
	}
	if config.HomeDirectory == "" {
		return nil, fmt.Errorf("home directory is empty")
	}
	if config.Getenv == nil {
		config.Getenv = os.Getenv
	}
	if config.LookupPath == nil {
		config.LookupPath = exec.LookPath
	}
	if config.RunCommand == nil {
		config.RunCommand = runCommand
	}
	if config.Stat == nil {
		config.Stat = os.Stat
	}
	command, err := filepath.Abs(config.Server.Command)
	if err != nil {
		return nil, err
	}
	server := config.Server
	server.Command = filepath.Clean(command)
	server.Args = append([]string(nil), server.Args...)
	dependencies := clientDependencies{
		home:       config.HomeDirectory,
		goos:       config.GOOS,
		getenv:     config.Getenv,
		lookupPath: config.LookupPath,
		run:        config.RunCommand,
		stat:       config.Stat,
	}
	manager := &Manager{
		server: server,
		clients: map[string]client{
			clientids.ClaudeCode:    newClaudeCodeClient(dependencies),
			clientids.ClaudeDesktop: newClaudeDesktopClient(dependencies),
			clientids.Cursor:        newCursorClient(dependencies),
			clientids.Antigravity:   newAntigravityClient(dependencies),
			clientids.Codex:         newCodexClient(dependencies),
			clientids.VSCode:        newVSCodeClient(dependencies),
		},
	}
	return manager, nil
}

// ServerDefinition returns a copy of the managed server definition.
func (m *Manager) ServerDefinition() ServerDefinition {
	result := m.server
	result.Args = append([]string(nil), result.Args...)
	return result
}

// List concurrently detects every supported client and reads its current
// registration state. Callers that present the result choose their own order.
func (m *Manager) List(ctx context.Context) []ClientStatus {
	statuses := make([]ClientStatus, 0, len(m.clients))
	results := make(chan ClientStatus, len(m.clients))
	for id, implementation := range m.clients {
		go func(id string, implementation client) {
			results <- m.status(ctx, id, implementation)
		}(id, implementation)
	}
	for range m.clients {
		statuses = append(statuses, <-results)
	}
	return statuses
}

func (m *Manager) status(ctx context.Context, id string, implementation client) ClientStatus {
	resolved, detectErr := implementation.Detect(ctx)
	state, differences, stateErr := implementation.RegistrationState(
		ctx,
		m.server,
		resolved,
	)
	status := statusFor(id, implementation, resolved, state, differences)
	if stateErr != nil {
		status.State = RegistrationStateUnreadable
		status.Err = installerError(ErrorConfigRead, id, stateErr)
	} else if detectErr != nil {
		status.Err = installerError(ErrorClientUnavailable, id, detectErr)
	}
	return status
}

// Status detects and reads one supported MCP client without inspecting others.
func (m *Manager) Status(ctx context.Context, id string) (ClientStatus, error) {
	implementation, ok := m.clients[id]
	if !ok {
		return ClientStatus{}, &InstallerError{
			Kind:     ErrorUnknownClient,
			ClientID: id,
		}
	}
	return m.status(ctx, id, implementation), nil
}

// Install adds the server to the selected automatically discovered client and
// verifies the resulting registration state.
func (m *Manager) Install(ctx context.Context, id string) (*InstallResult, error) {
	implementation, resolved, state, err := m.prepareOperation(ctx, id)
	if err != nil {
		return nil, err
	}
	if state == RegistrationStateConfigured {
		return &InstallResult{
			InstallOutcomeAlreadyConfigured,
			statusFor(id, implementation, resolved, state, nil),
		}, nil
	}
	state, differences, err := implementation.Install(ctx, m.server, resolved)
	if err != nil {
		return nil, mutationInstallerError(id, err)
	}
	if state != RegistrationStateConfigured {
		return nil, installerError(
			ErrorClientCommand,
			id,
			fmt.Errorf("client completed without updating the MCP server registration"),
		)
	}
	return &InstallResult{
		InstallOutcomeInstalled,
		statusFor(id, implementation, resolved, state, differences),
	}, nil
}

// Uninstall removes the exact server entry from the automatically discovered
// client.
func (m *Manager) Uninstall(ctx context.Context, id string) (*UninstallResult, error) {
	implementation, resolved, state, err := m.prepareOperation(ctx, id)
	if err != nil {
		return nil, err
	}
	if state == RegistrationStateNotConfigured {
		return &UninstallResult{
			UninstallOutcomeAlreadyAbsent,
			statusFor(id, implementation, resolved, state, nil),
		}, nil
	}
	state, differences, err := implementation.Uninstall(ctx, m.server, resolved)
	if err != nil {
		return nil, mutationInstallerError(id, err)
	}
	if state != RegistrationStateNotConfigured {
		return nil, installerError(
			ErrorClientCommand,
			id,
			fmt.Errorf("client completed without removing the MCP server registration"),
		)
	}
	return &UninstallResult{
		UninstallOutcomeRemoved,
		statusFor(id, implementation, resolved, state, differences),
	}, nil
}

// prepareOperation performs the checks shared by installation and removal.
// The caller remains responsible for the client-specific mutation and for
// verifying its resulting registration state.
func (m *Manager) prepareOperation(
	ctx context.Context,
	id string,
) (client, resolvedClient, RegistrationState, error) {
	implementation, ok := m.clients[id]
	if !ok {
		return nil, resolvedClient{}, 0, &InstallerError{
			Kind:     ErrorUnknownClient,
			ClientID: id,
		}
	}
	resolved, err := implementation.Detect(ctx)
	if err != nil || !resolved.detected {
		return nil, resolvedClient{}, 0, installerError(
			ErrorClientUnavailable,
			id,
			err,
		)
	}
	state, differences, err := implementation.RegistrationState(
		ctx,
		m.server,
		resolved,
	)
	if err != nil {
		return nil, resolvedClient{}, 0, installerError(
			ErrorConfigRead,
			id,
			err,
		)
	}
	if state == RegistrationStateConflict {
		return nil, resolvedClient{}, 0, conflictInstallerError(
			id,
			differences,
		)
	}
	return implementation, resolved, state, nil
}

func statusFor(
	id string,
	implementation client,
	resolved resolvedClient,
	state RegistrationState,
	differences []RegistrationDifference,
) ClientStatus {
	discovery := implementation.Discovery()
	return ClientStatus{
		ID:                      id,
		Detected:                resolved.detected,
		ExecutablePath:          resolved.executablePath,
		ConfigurationPath:       resolved.configurationPath,
		State:                   state,
		RegistrationDifferences: differences,
		DiscoveryCommands:       cloneStrings(discovery.Commands),
		DiscoveryPaths:          cloneStrings(discovery.Paths),
	}
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

func installerError(kind ErrorKind, id string, err error) error {
	result := &InstallerError{
		Kind:     kind,
		ClientID: id,
		Err:      err,
	}
	var commandFailure *clientCommandFailure
	if errors.As(err, &commandFailure) {
		result.CommandError = commandFailure.CommandError()
		result.CommandOutput = commandFailure.CommandOutput()
	}
	return result
}

func mutationInstallerError(id string, err error) error {
	var writeFailure *configurationWriteFailure
	if errors.As(err, &writeFailure) {
		return installerError(ErrorConfigWrite, id, err)
	}
	return installerError(ErrorClientCommand, id, err)
}

func conflictInstallerError(id string, differences []RegistrationDifference) error {
	return &InstallerError{
		Kind:                    ErrorConflict,
		ClientID:                id,
		RegistrationDifferences: append([]RegistrationDifference(nil), differences...),
	}
}

func runCommand(ctx context.Context, command string, args ...string) ([]byte, error) {
	return runCommandWithTimeout(ctx, clientCommandTimeout, command, args...)
}

func runCommandWithTimeout(
	ctx context.Context,
	timeout time.Duration,
	command string,
	args ...string,
) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output := &limitedBuffer{remaining: maxCommandOutput}
	// #nosec G702 -- client detection verifies that command is a regular file;
	// arguments are passed directly without invoking a shell.
	cmd := exec.CommandContext(commandCtx, command, args...)
	cmd.Stdout = output
	cmd.Stderr = output
	err := cmd.Run()
	if commandCtx.Err() != nil {
		err = commandCtx.Err()
	}
	return output.Bytes(), err
}

type limitedBuffer struct {
	bytes.Buffer
	remaining int
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	n := len(data)
	keep := min(len(data), b.remaining)
	if keep > 0 {
		_, _ = b.Buffer.Write(data[:keep])
		b.remaining -= keep
	}
	return n, nil
}
func executablePathsEqual(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
