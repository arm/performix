// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file contains shared process discovery and JSON configuration helpers
// used by the individual MCP client integrations.
package mcpclientinstaller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

func detectExecutable(
	ctx context.Context,
	d clientDependencies,
	names, paths []string,
	marker string,
	validate bool,
) (resolvedClient, error) {
	candidates := []string{}
	for _, name := range names {
		if path, err := d.lookupPath(name); err == nil {
			candidates = append(candidates, path)
		}
	}
	candidates = append(candidates, paths...)
	var last error
	for _, path := range candidates {
		info, err := d.stat(path)
		if err != nil || !info.Mode().IsRegular() {
			last = err
			continue
		}
		if validate {
			output, err := d.run(ctx, path, "--version")
			markerMissing := marker != "" && !strings.Contains(
				strings.ToLower(string(output)),
				strings.ToLower(marker),
			)
			if err != nil || markerMissing {
				last = err
				continue
			}
		}
		absolute := path
		if d.goos == runtime.GOOS {
			absolute, _ = filepath.Abs(path)
		}
		return resolvedClient{detected: true, executablePath: filepath.Clean(absolute)}, nil
	}
	return resolvedClient{}, last
}

func anyPathExists(d clientDependencies, paths []string) bool {
	for _, path := range paths {
		if _, err := d.stat(path); err == nil {
			return true
		}
	}
	return false
}

func runClientCommand(
	ctx context.Context,
	run CommandRunner,
	command string,
	args ...string,
) error {
	output, err := run(ctx, command, args...)
	if err != nil {
		return &clientCommandFailure{err: err, output: string(output)}
	}
	return nil
}

// clientCommandFailure keeps the process error separate from the client's
// combined output so callers can display that output without rewriting it.
type clientCommandFailure struct {
	err    error
	output string
}

type configurationWriteFailure struct {
	err error
}

func (e *configurationWriteFailure) Error() string { return e.err.Error() }
func (e *configurationWriteFailure) Unwrap() error { return e.err }

func configurationWriteError(err error) error {
	return &configurationWriteFailure{err: err}
}

func (e *clientCommandFailure) Error() string {
	if e.output == "" {
		return e.err.Error()
	}
	return fmt.Sprintf("%v: %s", e.err, e.output)
}

func (e *clientCommandFailure) Unwrap() error         { return e.err }
func (e *clientCommandFailure) CommandError() string  { return e.err.Error() }
func (e *clientCommandFailure) CommandOutput() string { return e.output }

func containsAbsent(output []byte) bool {
	text := strings.ToLower(string(output))
	return strings.Contains(text, "not found") ||
		strings.Contains(text, "no mcp server") ||
		strings.Contains(text, "does not exist")
}

func parseClaudeFields(output string) map[string]string {
	result := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok {
			result[key] = strings.TrimSpace(value)
		}
	}
	return result
}

type jsonServerEntry struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

func readJSONConfig(
	path, rootKey string,
) (map[string]json.RawMessage, map[string]json.RawMessage, fs.FileMode, error) {
	root, servers := map[string]json.RawMessage{}, map[string]json.RawMessage{}
	mode := fs.FileMode(0o600)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return root, servers, mode, nil
	}
	if err != nil {
		return nil, nil, 0, err
	}
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	// MCP clients can create their dedicated configuration file before any
	// servers are registered. An empty file therefore represents an empty
	// configuration, which installJSON will initialise with valid JSON.
	if len(bytes.TrimSpace(data)) == 0 {
		return root, servers, mode, nil
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, nil, 0, err
	}
	if root == nil {
		return nil, nil, 0, fmt.Errorf("MCP client configuration is not a JSON object")
	}
	if raw, ok := root[rootKey]; ok {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return nil, nil, 0, err
		}
		// A JSON null value is valid input and unmarshals to a nil map. Treat it
		// as an empty server collection so installJSON can safely add an entry.
		if servers == nil {
			servers = map[string]json.RawMessage{}
		}
	}
	return root, servers, mode, nil
}

func jsonRegistrationState(
	path, rootKey string,
	s ServerDefinition,
	includeType bool,
) (RegistrationState, []RegistrationDifference, error) {
	_, servers, _, err := readJSONConfig(path, rootKey)
	if err != nil {
		return RegistrationStateUnreadable, nil, err
	}
	raw, ok := servers[s.Name]
	if !ok {
		return RegistrationStateNotConfigured, nil, nil
	}
	var entry jsonServerEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return RegistrationStateUnreadable, nil, err
	}
	differences := registrationDifferences(
		entry.Type,
		entry.Command,
		entry.Args,
		entry.Env,
		s,
		includeType,
	)
	if len(differences) == 0 {
		return RegistrationStateConfigured, nil, nil
	}
	return RegistrationStateConflict, differences, nil
}

func registrationDifferences(
	actualType, actualCommand string,
	actualArgs []string,
	actualEnv map[string]string,
	expected ServerDefinition,
	includeType bool,
) []RegistrationDifference {
	var differences []RegistrationDifference
	if includeType && actualType != "" && actualType != "stdio" {
		differences = append(differences, RegistrationDifference{
			Field:    RegistrationDifferenceTransport,
			Actual:   actualType,
			Expected: "stdio",
		})
	}
	if !executablePathsEqual(actualCommand, expected.Command) {
		differences = append(differences, RegistrationDifference{
			Field:    RegistrationDifferenceCommand,
			Actual:   actualCommand,
			Expected: expected.Command,
		})
	}
	if !slices.Equal(actualArgs, expected.Args) {
		differences = append(differences, RegistrationDifference{
			Field:    RegistrationDifferenceArguments,
			Actual:   formatArguments(actualArgs),
			Expected: formatArguments(expected.Args),
		})
	}
	if len(actualEnv) > 0 {
		differences = append(differences, RegistrationDifference{
			Field:    RegistrationDifferenceEnvironment,
			Actual:   environmentVariableNames(actualEnv),
			Expected: "none",
		})
	}
	return differences
}

func formatArguments(args []string) string {
	encoded, err := json.Marshal(args)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

func environmentVariableNames(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "none"
	}
	return strings.Join(keys, ", ")
}

func installJSON(path, rootKey string, s ServerDefinition, includeType bool) error {
	// Preserve unrelated top-level properties and server entries. The atomic
	// writer also retains the existing file mode when replacing the file.
	root, servers, mode, err := readJSONConfig(path, rootKey)
	if err != nil {
		return err
	}
	entry := jsonServerEntry{Command: s.Command, Args: append([]string{}, s.Args...)}
	if includeType {
		entry.Type = "stdio"
	}
	raw, _ := json.Marshal(entry)
	servers[s.Name] = raw
	root[rootKey], _ = json.Marshal(servers)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return util.WriteJSONFileAtomic(path, &root, mode)
}
func uninstallJSON(path, rootKey, name string) error {
	root, servers, mode, err := readJSONConfig(path, rootKey)
	if err != nil {
		return err
	}
	delete(servers, name)
	root[rootKey], _ = json.Marshal(servers)
	return util.WriteJSONFileAtomic(path, &root, mode)
}
