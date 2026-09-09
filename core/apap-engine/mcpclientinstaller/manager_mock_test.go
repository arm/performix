// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file tests MCP client manager behaviour with deterministic client and
// process substitutes.
package mcpclientinstaller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/mcpclientinstaller/clientids"
)

const testServerName = "test-server"

func testServer() ServerDefinition {
	return ServerDefinition{
		Name:    testServerName,
		Command: filepath.Join(string(filepath.Separator), "opt", "example", "mcp-server"),
		Args:    []string{"serve"},
	}
}

func mockExecutable(name string) string {
	return filepath.Join(string(filepath.Separator), "mock", "bin", name)
}

type fakeClients struct {
	paths            map[string]bool
	pathModes        map[string]os.FileMode
	server           ServerDefinition
	claudeConfigured bool
	codexConfigured  bool
	vscodeConfigPath string
	commandsMutex    sync.Mutex
	commands         [][]string
}

func (f *fakeClients) stat(path string) (os.FileInfo, error) {
	if f.paths[path] {
		return fakeFileInfo{name: filepath.Base(path), mode: f.pathModes[path]}, nil
	}
	return nil, os.ErrNotExist
}

func (f *fakeClients) run(_ context.Context, command string, args ...string) ([]byte, error) {
	f.commandsMutex.Lock()
	f.commands = append(f.commands, append([]string{command}, args...))
	f.commandsMutex.Unlock()
	commandName := strings.ToLower(
		strings.TrimSuffix(filepath.Base(command), filepath.Ext(command)),
	)
	if len(args) == 1 && args[0] == "--version" {
		if commandName == "claude" {
			return []byte("Claude Code 2.0"), nil
		}
		return []byte("codex-cli 1.0"), nil
	}
	if commandName == "claude" && len(args) >= 3 && args[0] == "mcp" && args[1] == "get" {
		if !f.claudeConfigured {
			return []byte("No MCP server found"), errors.New("exit 1")
		}
		return []byte(fmt.Sprintf(
			"%s:\n  Type: stdio\n  Command: %s\n  Args: %s\n",
			f.server.Name,
			f.server.Command,
			strings.Join(f.server.Args, " "),
		)), nil
	}
	if commandName == "claude" && len(args) >= 2 && args[0] == "mcp" && args[1] == "add" {
		f.claudeConfigured = true
	}
	if commandName == "claude" && len(args) >= 2 && args[0] == "mcp" && args[1] == "remove" {
		f.claudeConfigured = false
	}
	if commandName == "codex" && len(args) >= 3 && args[0] == "mcp" && args[1] == "get" {
		if !f.codexConfigured {
			return []byte("MCP server not found"), errors.New("exit 1")
		}
		var configuration codexServerConfiguration
		configuration.Transport.Type = "stdio"
		configuration.Transport.Command = f.server.Command
		configuration.Transport.Args = f.server.Args
		return json.Marshal(configuration)
	}
	if commandName == "codex" && len(args) >= 2 && args[0] == "mcp" && args[1] == "add" {
		f.codexConfigured = true
	}
	if commandName == "codex" && len(args) >= 2 && args[0] == "mcp" && args[1] == "remove" {
		f.codexConfigured = false
	}
	if commandName == "code" && len(args) == 2 && args[0] == "--add-mcp" {
		var definition struct {
			Name    string   `json:"name"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
		}
		if err := json.Unmarshal([]byte(args[1]), &definition); err != nil {
			return nil, err
		}
		server := ServerDefinition{
			Name:    definition.Name,
			Command: definition.Command,
			Args:    definition.Args,
		}
		if err := installJSON(f.vscodeConfigPath, "servers", server, true); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

type fakeFileInfo struct {
	name string
	mode os.FileMode
}

func (f fakeFileInfo) Name() string      { return f.name }
func (fakeFileInfo) Size() int64         { return 0 }
func (f fakeFileInfo) Mode() os.FileMode { return f.mode }
func (fakeFileInfo) ModTime() time.Time  { return time.Time{} }
func (fakeFileInfo) IsDir() bool         { return false }
func (fakeFileInfo) Sys() any            { return nil }

type blockingClient struct {
	id      string
	started chan<- string
	release <-chan struct{}
}

type mutationFailureClient struct {
	client
	err error
}

func (c mutationFailureClient) Install(
	context.Context,
	ServerDefinition,
	resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	return RegistrationStateUnreadable, nil, c.err
}

func (blockingClient) Discovery() ClientDiscovery { return ClientDiscovery{} }

func (c blockingClient) Detect(ctx context.Context) (resolvedClient, error) {
	c.started <- c.id
	select {
	case <-c.release:
		return resolvedClient{detected: true}, nil
	case <-ctx.Done():
		return resolvedClient{}, ctx.Err()
	}
}

func (blockingClient) RegistrationState(
	context.Context,
	ServerDefinition,
	resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	return RegistrationStateNotConfigured, nil, nil
}

func (blockingClient) Install(
	context.Context,
	ServerDefinition,
	resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	return RegistrationStateConfigured, nil, nil
}

func (blockingClient) Uninstall(
	context.Context,
	ServerDefinition,
	resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	return RegistrationStateNotConfigured, nil, nil
}

func newTestManager(t *testing.T, fake *fakeClients) *Manager {
	t.Helper()
	home := t.TempDir()
	fake.paths["/Applications/Claude.app"] = true
	fake.paths["/Applications/Cursor.app"] = true
	fake.paths["/Applications/Antigravity.app"] = true
	m, err := NewWithConfig(
		Config{
			Server:        testServer(),
			HomeDirectory: home,
			GOOS:          "darwin",
			Getenv:        func(string) string { return "" },
			LookupPath: func(name string) (string, error) {
				path := mockExecutable(name)
				if fake.paths[path] {
					return path, nil
				}
				return "", os.ErrNotExist
			},
			RunCommand: fake.run,
			Stat:       fake.stat,
		},
	)
	require.NoError(t, err)
	fake.server = m.ServerDefinition()
	return m
}

func newPlatformTestManager(
	t *testing.T,
	goos string,
	home string,
	environment map[string]string,
	commands map[string]string,
	fake *fakeClients,
) *Manager {
	t.Helper()
	manager, err := NewWithConfig(Config{
		Server:        testServer(),
		HomeDirectory: home,
		GOOS:          goos,
		Getenv:        func(name string) string { return environment[name] },
		LookupPath: func(command string) (string, error) {
			if path := commands[command]; path != "" {
				return path, nil
			}
			return "", os.ErrNotExist
		},
		RunCommand: fake.run,
		Stat:       fake.stat,
	})
	require.NoError(t, err)
	fake.server = manager.ServerDefinition()
	return manager
}

func clientStatus(t *testing.T, manager *Manager, id string) ClientStatus {
	t.Helper()
	for _, status := range manager.List(context.Background()) {
		if status.ID == id {
			return status
		}
	}
	t.Fatalf("client %q was not listed", id)
	return ClientStatus{}
}

func TestRunCommandTimesOut(t *testing.T) {
	const helperArgument = "mcp-command-timeout-helper"
	for _, argument := range os.Args {
		if argument == helperArgument {
			time.Sleep(time.Second)
			return
		}
	}

	started := time.Now()
	_, err := runCommandWithTimeout(
		context.Background(),
		20*time.Millisecond,
		os.Args[0],
		"-test.run=TestRunCommandTimesOut",
		"--",
		helperArgument,
	)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(started), time.Second)
}

func TestManagerListsConcreteClientsAndDiscoveryDetails(t *testing.T) {
	fake := &fakeClients{
		paths: map[string]bool{
			mockExecutable("claude"): true,
			mockExecutable("codex"):  true,
			"/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code": true,
		},
	}
	m := newTestManager(t, fake)
	statuses := m.List(context.Background())
	require.Len(t, statuses, 6)
	assert.ElementsMatch(
		t,
		clientids.All(),
		[]string{
			statuses[0].ID,
			statuses[1].ID,
			statuses[2].ID,
			statuses[3].ID,
			statuses[4].ID,
			statuses[5].ID,
		},
	)
	statusesByID := make(map[string]ClientStatus, len(statuses))
	for _, status := range statuses {
		statusesByID[status.ID] = status
	}
	assert.Equal(t, []string{"codex"}, statusesByID["codex"].DiscoveryCommands)
	assert.Contains(t, statusesByID["codex"].DiscoveryPaths, "/usr/local/bin/codex")
}

func TestUnknownStatusClientReturnsInstallerError(t *testing.T) {
	m := newTestManager(t, &fakeClients{paths: map[string]bool{}})

	_, err := m.Status(context.Background(), "unknown-client")
	var installerErr *InstallerError
	require.ErrorAs(t, err, &installerErr)
	assert.Equal(t, ErrorUnknownClient, installerErr.Kind)
	assert.Equal(t, "unknown-client", installerErr.ClientID)
}

func TestManagerListsClientsConcurrently(t *testing.T) {
	started := make(chan string, 3)
	release := make(chan struct{})
	manager := &Manager{
		server: testServer(),
		clients: map[string]client{
			"first":  blockingClient{id: "first", started: started, release: release},
			"second": blockingClient{id: "second", started: started, release: release},
			"third":  blockingClient{id: "third", started: started, release: release},
		},
	}

	result := make(chan []ClientStatus, 1)
	go func() { result <- manager.List(context.Background()) }()

	for range manager.clients {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("not all client inspections started concurrently")
		}
	}
	close(release)

	statuses := <-result
	assert.ElementsMatch(
		t,
		[]string{"first", "second", "third"},
		[]string{statuses[0].ID, statuses[1].ID, statuses[2].ID},
	)
}

func TestLinuxDefaultClientPaths(t *testing.T) {
	home := "/home/test"
	configHome := filepath.Join(home, ".config")
	fake := &fakeClients{paths: map[string]bool{
		filepath.Join(configHome, "Claude"):           true,
		filepath.Join(home, ".cursor"):                true,
		filepath.Join(home, ".gemini", "antigravity"): true,
		"/usr/bin/claude-desktop":                     true,
		"/usr/bin/cursor":                             true,
		"/usr/local/bin/claude":                       true,
		"/usr/local/bin/codex":                        true,
		"/usr/bin/code":                               true,
	}}
	manager := newPlatformTestManager(t, "linux", home, map[string]string{
		"XDG_CONFIG_HOME": configHome,
	}, nil, fake)

	claudeCode := clientStatus(t, manager, "claude-code")
	assert.True(t, claudeCode.Detected)
	assert.Equal(t, filepath.Clean("/usr/local/bin/claude"), claudeCode.ExecutablePath)

	codex := clientStatus(t, manager, "codex")
	assert.True(t, codex.Detected)
	assert.Equal(t, filepath.Clean("/usr/local/bin/codex"), codex.ExecutablePath)

	claudeDesktop := clientStatus(t, manager, "claude-desktop")
	assert.True(t, claudeDesktop.Detected)
	assert.Equal(t, filepath.Clean("/usr/bin/claude-desktop"), claudeDesktop.ExecutablePath)
	assert.Equal(
		t,
		filepath.Join(configHome, "Claude", "claude_desktop_config.json"),
		claudeDesktop.ConfigurationPath,
	)

	cursor := clientStatus(t, manager, "cursor")
	assert.True(t, cursor.Detected)
	assert.Equal(t, filepath.Clean("/usr/bin/cursor"), cursor.ExecutablePath)
	assert.Equal(t, filepath.Join(home, ".cursor", "mcp.json"), cursor.ConfigurationPath)

	antigravity := clientStatus(t, manager, "antigravity")
	assert.True(t, antigravity.Detected)
	assert.Equal(
		t,
		filepath.Join(home, ".gemini", "config", "mcp_config.json"),
		antigravity.ConfigurationPath,
	)

	vscode := clientStatus(t, manager, "vscode")
	assert.True(t, vscode.Detected)
	assert.Equal(t, filepath.Clean("/usr/bin/code"), vscode.ExecutablePath)
	assert.Equal(t, filepath.Join(configHome, "Code", "User", "mcp.json"), vscode.ConfigurationPath)
}

func TestLinuxAntigravityIgnoresRetainedConfigurationDirectory(t *testing.T) {
	home := "/home/test"
	fake := &fakeClients{paths: map[string]bool{
		filepath.Join(home, ".gemini", "config"): true,
	}}
	manager := newPlatformTestManager(t, "linux", home, nil, nil, fake)

	status := clientStatus(t, manager, "antigravity")
	assert.False(t, status.Detected)
	assert.Equal(
		t,
		filepath.Join(home, ".gemini", "config", "mcp_config.json"),
		status.ConfigurationPath,
	)
}

func TestLinuxClaudeDesktopSupportsLowerCaseConfigDirectory(t *testing.T) {
	home := "/home/test"
	configHome := filepath.Join(home, ".config")
	legacyDirectory := filepath.Join(configHome, "claude")
	fake := &fakeClients{paths: map[string]bool{
		legacyDirectory:           true,
		"/usr/bin/claude-desktop": true,
	}}
	manager := newPlatformTestManager(t, "linux", home, map[string]string{
		"XDG_CONFIG_HOME": configHome,
	}, nil, fake)

	status := clientStatus(t, manager, "claude-desktop")
	assert.True(t, status.Detected)
	assert.Equal(
		t,
		filepath.Join(legacyDirectory, "claude_desktop_config.json"),
		status.ConfigurationPath,
	)
}

func TestLinuxClaudeDesktopReevaluatesConfigDirectory(t *testing.T) {
	home := "/home/test"
	configHome := filepath.Join(home, ".config")
	lowerCaseDirectory := filepath.Join(configHome, "claude")
	fake := &fakeClients{paths: map[string]bool{
		"/usr/bin/claude-desktop": true,
	}}
	manager := newPlatformTestManager(t, "linux", home, map[string]string{
		"XDG_CONFIG_HOME": configHome,
	}, nil, fake)

	initial := clientStatus(t, manager, "claude-desktop")
	assert.True(t, initial.Detected)
	assert.Equal(
		t,
		filepath.Join(configHome, "Claude", "claude_desktop_config.json"),
		initial.ConfigurationPath,
	)

	fake.paths[lowerCaseDirectory] = true
	installed := clientStatus(t, manager, "claude-desktop")
	assert.True(t, installed.Detected)
	assert.Equal(
		t,
		filepath.Join(lowerCaseDirectory, "claude_desktop_config.json"),
		installed.ConfigurationPath,
	)
}

func TestLinuxRetainedClientDataIsNotInstallationEvidence(t *testing.T) {
	home := "/home/test"
	configHome := filepath.Join(home, ".config")
	fake := &fakeClients{paths: map[string]bool{
		filepath.Join(configHome, "Claude"): true,
		filepath.Join(home, ".cursor"):      true,
	}}
	manager := newPlatformTestManager(t, "linux", home, map[string]string{
		"XDG_CONFIG_HOME": configHome,
	}, nil, fake)

	assert.False(t, clientStatus(t, manager, "claude-desktop").Detected)
	assert.False(t, clientStatus(t, manager, "cursor").Detected)
}

func TestWindowsDefaultClientPaths(t *testing.T) {
	home := `C:\Users\test`
	appData := filepath.Join(home, "AppData", "Roaming")
	localAppData := filepath.Join(home, "AppData", "Local")
	programFiles := `C:\Program Files`
	claudeDesktopExecutable := filepath.Join(
		localAppData,
		"Microsoft",
		"WindowsApps",
		"claude.exe",
	)
	vscodeExecutable := filepath.Join(localAppData, "Programs", "Microsoft VS Code", "Code.exe")
	claudeExecutable := filepath.Join(home, ".local", "bin", "claude.exe")
	codexExecutable := filepath.Join(
		localAppData,
		"Programs",
		"OpenAI",
		"Codex",
		"bin",
		"codex.exe",
	)
	cursorExecutable := filepath.Join(localAppData, "Programs", "cursor", "Cursor.exe")
	antigravityExecutable := filepath.Join(localAppData, "agy", "bin", "agy.exe")
	fake := &fakeClients{paths: map[string]bool{
		filepath.Join(appData, "Claude"): true,
		claudeDesktopExecutable:          true,
		cursorExecutable:                 true,
		antigravityExecutable:            true,
		vscodeExecutable:                 true,
		claudeExecutable:                 true,
		codexExecutable:                  true,
	}}
	manager := newPlatformTestManager(t, "windows", home, map[string]string{
		"APPDATA":      appData,
		"LOCALAPPDATA": localAppData,
		"ProgramFiles": programFiles,
	}, nil, fake)

	claudeCode := clientStatus(t, manager, "claude-code")
	assert.True(t, claudeCode.Detected)
	assert.Equal(t, claudeExecutable, claudeCode.ExecutablePath)

	codex := clientStatus(t, manager, "codex")
	assert.True(t, codex.Detected)
	assert.Equal(t, codexExecutable, codex.ExecutablePath)

	claudeDesktop := clientStatus(t, manager, "claude-desktop")
	assert.True(t, claudeDesktop.Detected)
	assert.Empty(t, claudeDesktop.ExecutablePath)
	assert.Equal(
		t,
		filepath.Join(appData, "Claude", "claude_desktop_config.json"),
		claudeDesktop.ConfigurationPath,
	)

	cursor := clientStatus(t, manager, "cursor")
	assert.True(t, cursor.Detected)
	assert.Equal(t, filepath.Join(home, ".cursor", "mcp.json"), cursor.ConfigurationPath)

	antigravity := clientStatus(t, manager, "antigravity")
	assert.True(t, antigravity.Detected)
	assert.Equal(
		t,
		filepath.Join(home, ".gemini", "config", "mcp_config.json"),
		antigravity.ConfigurationPath,
	)

	vscode := clientStatus(t, manager, "vscode")
	assert.True(t, vscode.Detected)
	assert.Equal(t, vscodeExecutable, vscode.ExecutablePath)
	assert.Equal(t, filepath.Join(appData, "Code", "User", "mcp.json"), vscode.ConfigurationPath)
}

func TestWindowsRetainedClientDataIsNotInstallationEvidence(t *testing.T) {
	home := `C:\Users\test`
	appData := filepath.Join(home, "AppData", "Roaming")
	localAppData := filepath.Join(home, "AppData", "Local")
	fake := &fakeClients{paths: map[string]bool{
		filepath.Join(appData, "Claude"):              true,
		filepath.Join(appData, "Cursor"):              true,
		filepath.Join(home, ".gemini", "antigravity"): true,
	}}
	manager := newPlatformTestManager(t, "windows", home, map[string]string{
		"APPDATA":      appData,
		"LOCALAPPDATA": localAppData,
	}, nil, fake)

	assert.False(t, clientStatus(t, manager, "cursor").Detected)
	assert.False(t, clientStatus(t, manager, "claude-desktop").Detected)
	assert.False(t, clientStatus(t, manager, "antigravity").Detected)
}

func TestWindowsClaudeCodeIgnoresClaudeDesktopAlias(t *testing.T) {
	home := `C:\Users\test`
	appData := filepath.Join(home, "AppData", "Roaming")
	localAppData := filepath.Join(home, "AppData", "Local")
	desktopAlias := filepath.Join(localAppData, "Microsoft", "WindowsApps", "Claude.exe")
	fake := &fakeClients{paths: map[string]bool{desktopAlias: true}}
	manager := newPlatformTestManager(t, "windows", home, map[string]string{
		"APPDATA":      appData,
		"LOCALAPPDATA": localAppData,
	}, map[string]string{"claude": desktopAlias}, fake)

	status := clientStatus(t, manager, "claude-code")
	assert.False(t, status.Detected)
	require.Error(t, status.Err)
}

func TestWindowsClaudeDesktopDetectsMSIXApplicationAlias(t *testing.T) {
	home := `C:\Users\test`
	appData := filepath.Join(home, "AppData", "Roaming")
	localAppData := filepath.Join(home, "AppData", "Local")
	desktopAlias := filepath.Join(localAppData, "Microsoft", "WindowsApps", "claude.exe")
	fake := &fakeClients{
		paths:     map[string]bool{desktopAlias: true},
		pathModes: map[string]os.FileMode{desktopAlias: os.ModeIrregular},
	}
	manager := newPlatformTestManager(t, "windows", home, map[string]string{
		"APPDATA":      appData,
		"LOCALAPPDATA": localAppData,
	}, nil, fake)

	status := clientStatus(t, manager, "claude-desktop")
	assert.True(t, status.Detected)
	assert.Empty(t, status.ExecutablePath)
}

func TestWindowsAlternativeClientPaths(t *testing.T) {
	home := `C:\Users\test`
	appData := filepath.Join(home, "AppData", "Roaming")
	localAppData := filepath.Join(home, "AppData", "Local")
	programFiles := `C:\Program Files`
	environment := map[string]string{
		"APPDATA":      appData,
		"LOCALAPPDATA": localAppData,
		"ProgramFiles": programFiles,
	}
	tests := []struct {
		name, id, executable, command string
	}{
		{
			"Claude Desktop unpackaged",
			"claude-desktop",
			filepath.Join(localAppData, "Programs", "Claude", "Claude.exe"),
			"",
		},
		{"Claude Code npm", "claude-code", filepath.Join(appData, "npm", "claude.cmd"), ""},
		{"Codex npm", "codex", filepath.Join(appData, "npm", "codex.cmd"), ""},
		{"Cursor system", "cursor", filepath.Join(programFiles, "Cursor", "Cursor.exe"), ""},
		{
			"Antigravity IDE",
			"antigravity",
			filepath.Join(localAppData, "Programs", "antigravity-ide", "Antigravity IDE.exe"),
			"",
		},
		{
			"Antigravity legacy IDE",
			"antigravity",
			filepath.Join(localAppData, "Programs", "Antigravity", "Antigravity.exe"),
			"",
		},
		{"Antigravity CLI on PATH", "antigravity", filepath.Join(home, "bin", "agy.exe"), "agy"},
		{
			"VS Code system",
			"vscode",
			filepath.Join(programFiles, "Microsoft VS Code", "Code.exe"),
			"",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeClients{paths: map[string]bool{test.executable: true}}
			commands := map[string]string{}
			if test.command != "" {
				commands[test.command] = test.executable
			}
			manager := newPlatformTestManager(
				t,
				"windows",
				home,
				environment,
				commands,
				fake,
			)

			status := clientStatus(t, manager, test.id)
			assert.True(t, status.Detected)
			assert.NoError(t, status.Err)
		})
	}
}

func TestCursorInstallPreservesUnrelatedConfiguration(t *testing.T) {
	fake := &fakeClients{paths: map[string]bool{}}
	m := newTestManager(t, fake)
	cursor := m.clients["cursor"].(*cursorClient)
	require.NoError(t, os.MkdirAll(filepath.Dir(cursor.configPath), 0o700))
	require.NoError(
		t,
		os.WriteFile(
			cursor.configPath,
			[]byte(`{"theme":"dark","mcpServers":{"other":{"command":"other","args":[]}}}`),
			0o640,
		),
	)
	result, err := m.Install(context.Background(), "cursor")
	require.NoError(t, err)
	assert.Equal(t, InstallOutcomeInstalled, result.Outcome)
	data, err := os.ReadFile(cursor.configPath)
	require.NoError(t, err)
	var root map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &root))
	assert.JSONEq(t, `"dark"`, string(root["theme"]))
	var servers map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(root["mcpServers"], &servers))
	assert.Contains(t, servers, "other")
	assert.Contains(t, servers, testServerName)

	again, err := m.Install(context.Background(), "cursor")
	require.NoError(t, err)
	assert.Equal(t, InstallOutcomeAlreadyConfigured, again.Outcome)

	removed, err := m.Uninstall(context.Background(), "cursor")
	require.NoError(t, err)
	assert.Equal(t, RegistrationStateNotConfigured, removed.Status.State)

	data, err = os.ReadFile(cursor.configPath)
	require.NoError(t, err)
	root = make(map[string]json.RawMessage)
	require.NoError(t, json.Unmarshal(data, &root))
	servers = make(map[string]json.RawMessage)
	require.NoError(t, json.Unmarshal(root["mcpServers"], &servers))
	assert.Contains(t, servers, "other")
	assert.NotContains(t, servers, testServerName)
}

func TestConflictingOperationsIncludeRegistrationDifferences(t *testing.T) {
	fake := &fakeClients{paths: map[string]bool{}}
	manager := newTestManager(t, fake)
	cursor := manager.clients[clientids.Cursor].(*cursorClient)
	require.NoError(t, os.MkdirAll(filepath.Dir(cursor.configPath), 0o700))
	require.NoError(
		t,
		os.WriteFile(
			cursor.configPath,
			[]byte(`{"mcpServers":{"test-server":{"command":"/old/apx","args":["serve"]}}}`),
			0o600,
		),
	)

	operations := map[string]func(context.Context, string) error{
		"install": func(ctx context.Context, id string) error {
			_, err := manager.Install(ctx, id)
			return err
		},
		"uninstall": func(ctx context.Context, id string) error {
			_, err := manager.Uninstall(ctx, id)
			return err
		},
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			err := operation(context.Background(), clientids.Cursor)
			var installerErr *InstallerError
			require.ErrorAs(t, err, &installerErr)
			assert.Equal(t, ErrorConflict, installerErr.Kind)
			assert.Equal(t, []RegistrationDifference{{
				Field:    RegistrationDifferenceCommand,
				Actual:   "/old/apx",
				Expected: manager.ServerDefinition().Command,
			}}, installerErr.RegistrationDifferences)
		})
	}
}

func TestDirectConfigurationWriteFailureIsClassified(t *testing.T) {
	fake := &fakeClients{paths: map[string]bool{}}
	manager := newTestManager(t, fake)
	cursor := manager.clients[clientids.Cursor]
	manager.clients[clientids.Cursor] = mutationFailureClient{
		client: cursor,
		err:    configurationWriteError(os.ErrPermission),
	}

	result, err := manager.Install(context.Background(), clientids.Cursor)
	require.Nil(t, result)
	var installerErr *InstallerError
	require.ErrorAs(t, err, &installerErr)
	assert.Equal(t, ErrorConfigWrite, installerErr.Kind)
	assert.ErrorIs(t, installerErr, os.ErrPermission)
}

func TestDetectionIsRecalculatedForEveryList(t *testing.T) {
	fake := &fakeClients{paths: map[string]bool{}}
	m := newTestManager(t, fake)
	assert.True(t, clientStatus(t, m, "cursor").Detected)
	delete(fake.paths, "/Applications/Cursor.app")
	assert.False(t, clientStatus(t, m, "cursor").Detected)
}

func TestClaudeCodeUsesNativeMCPCommands(t *testing.T) {
	claudeExecutable := mockExecutable("claude")
	fake := &fakeClients{paths: map[string]bool{claudeExecutable: true}}
	m := newTestManager(t, fake)
	installed, err := m.Install(context.Background(), "claude-code")
	require.NoError(t, err)
	assert.Equal(t, RegistrationStateConfigured, installed.Status.State)
	removed, err := m.Uninstall(context.Background(), "claude-code")
	require.NoError(t, err)
	assert.Equal(t, RegistrationStateNotConfigured, removed.Status.State)
	assert.Contains(t, fake.commands, []string{
		claudeExecutable,
		"mcp", "add", "--transport", "stdio", "--scope", "user",
		testServerName, "--", m.ServerDefinition().Command, "serve",
	})
	assert.Contains(t, fake.commands, []string{
		claudeExecutable,
		"mcp", "remove", testServerName, "--scope", "user",
	})
	var getCommands int
	for _, command := range fake.commands {
		assert.NotEqual(t, []string{claudeExecutable, "--version"}, command)
		if len(command) > 2 && command[1] == "mcp" && command[2] == "get" {
			getCommands++
		}
	}
	assert.Equal(
		t,
		2,
		getCommands,
		"Claude Code should not re-check status after a native mutation",
	)
}

func TestCodexUsesNativeMCPCommands(t *testing.T) {
	codexExecutable := mockExecutable("codex")
	fake := &fakeClients{paths: map[string]bool{codexExecutable: true}}
	m := newTestManager(t, fake)

	installed, err := m.Install(context.Background(), "codex")
	require.NoError(t, err)
	assert.Equal(t, RegistrationStateConfigured, installed.Status.State)

	removed, err := m.Uninstall(context.Background(), "codex")
	require.NoError(t, err)
	assert.Equal(t, RegistrationStateNotConfigured, removed.Status.State)

	assert.Contains(t, fake.commands, []string{
		codexExecutable, "mcp", "add", testServerName, "--",
		m.ServerDefinition().Command, "serve",
	})
	assert.Contains(t, fake.commands, []string{
		codexExecutable, "mcp", "remove", testServerName,
	})
	assert.Contains(t, fake.commands, []string{
		codexExecutable, "mcp", "get", testServerName, "--json",
	})
}

func TestAntigravityLifecycleUsesGlobalConfiguration(t *testing.T) {
	fake := &fakeClients{paths: map[string]bool{}}
	m := newTestManager(t, fake)

	installed, err := m.Install(context.Background(), "antigravity")
	require.NoError(t, err)
	assert.Equal(t, RegistrationStateConfigured, installed.Status.State)

	again, err := m.Install(context.Background(), "antigravity")
	require.NoError(t, err)
	assert.Equal(t, InstallOutcomeAlreadyConfigured, again.Outcome)

	removed, err := m.Uninstall(context.Background(), "antigravity")
	require.NoError(t, err)
	assert.Equal(t, RegistrationStateNotConfigured, removed.Status.State)
}

func TestVSCodeLifecycleUsesNativeAddAndDirectRemoval(t *testing.T) {
	fake := &fakeClients{paths: map[string]bool{
		"/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code": true,
	}}
	m := newTestManager(t, fake)
	vscode := m.clients["vscode"].(*vscodeClient)
	fake.vscodeConfigPath = vscode.configPath

	installed, err := m.Install(context.Background(), "vscode")
	require.NoError(t, err)
	assert.Equal(t, RegistrationStateConfigured, installed.Status.State)

	again, err := m.Install(context.Background(), "vscode")
	require.NoError(t, err)
	assert.Equal(t, InstallOutcomeAlreadyConfigured, again.Outcome)

	removed, err := m.Uninstall(context.Background(), "vscode")
	require.NoError(t, err)
	assert.Equal(t, RegistrationStateNotConfigured, removed.Status.State)

	var addCommands int
	for _, command := range fake.commands {
		if len(command) > 2 && command[1] == "--add-mcp" {
			addCommands++
		}
	}
	assert.Equal(t, 1, addCommands)
}

func TestMalformedConfigurationIsUnreadable(t *testing.T) {
	fake := &fakeClients{paths: map[string]bool{}}
	m := newTestManager(t, fake)
	cursor := m.clients["cursor"].(*cursorClient)
	require.NoError(t, os.MkdirAll(filepath.Dir(cursor.configPath), 0o700))
	require.NoError(t, os.WriteFile(cursor.configPath, []byte(`{"mcpServers":`), 0o600))
	assert.Equal(t, RegistrationStateUnreadable, clientStatus(t, m, "cursor").State)
}
