// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file integrates Claude Code through its documented MCP CLI commands.
package mcpclientinstaller

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

func claudePaths(dependencies clientDependencies) []string {
	// Anthropic documents the native Windows binary under ~/.local/bin and also
	// supports npm installations:
	// https://code.claude.com/docs/en/troubleshoot-install
	if dependencies.goos == "windows" {
		return []string{
			filepath.Join(dependencies.home, ".local", "bin", "claude.exe"),
			filepath.Join(dependencies.getenv("APPDATA"), "npm", "claude.cmd"),
		}
	}
	return []string{
		filepath.Join(dependencies.home, ".claude", "local", "claude"),
		"/usr/local/bin/claude",
		filepath.Join(dependencies.home, ".npm-global", "bin", "claude"),
	}
}

type claudeCodeClient struct {
	dependencies clientDependencies
}

var _ client = (*claudeCodeClient)(nil)

func newClaudeCodeClient(dependencies clientDependencies) *claudeCodeClient {
	return &claudeCodeClient{dependencies: dependencies}
}

func (c *claudeCodeClient) Discovery() ClientDiscovery {
	return ClientDiscovery{Commands: []string{"claude"}, Paths: claudePaths(c.dependencies)}
}

func (c *claudeCodeClient) Detect(ctx context.Context) (resolvedClient, error) {
	// The subsequent `claude mcp get` is the relevant compatibility check for
	// registration. Avoid a separate version command before every operation.
	// Claude Desktop registers a WindowsApps alias named claude.exe. Prefer
	// Claude Code's documented native and npm locations so that alias is not
	// mistaken for the CLI when it appears first on PATH.
	paths := claudePaths(c.dependencies)
	names := []string{"claude"}
	if c.dependencies.goos == "windows" {
		names = nil
		if path, err := c.dependencies.lookupPath("claude"); err == nil &&
			!isClaudeDesktopAlias(path) {
			paths = append([]string{path}, paths...)
		}
	}
	return detectExecutable(
		ctx,
		c.dependencies,
		names,
		paths,
		"Claude Code",
		false,
	)
}

func isClaudeDesktopAlias(path string) bool {
	normalised := strings.ToLower(strings.ReplaceAll(path, `\`, "/"))
	return strings.HasSuffix(normalised, "/microsoft/windowsapps/claude.exe")
}

func (c *claudeCodeClient) RegistrationState(
	ctx context.Context,
	server ServerDefinition,
	resolved resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	if !resolved.detected {
		return RegistrationStateNotConfigured, nil, nil
	}

	output, err := c.dependencies.run(ctx, resolved.executablePath, "mcp", "get", server.Name)
	if err != nil {
		if containsAbsent(output) {
			return RegistrationStateNotConfigured, nil, nil
		}
		return RegistrationStateUnreadable, nil, fmt.Errorf("%w: %s", err, output)
	}

	// Claude documents `mcp get`, `mcp add` and `mcp remove` as the supported
	// management interface: https://docs.anthropic.com/en/docs/claude-code/mcp
	// It has no machine-readable form of `mcp get`. Parse only the
	// stable Command and Args fields needed to identify our registration; ignore
	// its connection-health output because client health is outside this package.
	fields := parseClaudeFields(string(output))
	differences := registrationDifferences(
		"",
		fields["Command"],
		strings.Fields(fields["Args"]),
		nil,
		server,
		false,
	)
	if len(differences) == 0 {
		return RegistrationStateConfigured, nil, nil
	}
	return RegistrationStateConflict, differences, nil
}

func (c *claudeCodeClient) Install(
	ctx context.Context,
	server ServerDefinition,
	resolved resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	args := []string{"mcp", "add", "--transport", "stdio", "--scope", "user"}
	args = append(args, server.Name, "--", server.Command)
	args = append(args, server.Args...)
	if err := runClientCommand(ctx, c.dependencies.run, resolved.executablePath, args...); err != nil {
		return RegistrationStateUnreadable, nil, err
	}
	return RegistrationStateConfigured, nil, nil
}

func (c *claudeCodeClient) Uninstall(
	ctx context.Context,
	server ServerDefinition,
	resolved resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	err := runClientCommand(
		ctx,
		c.dependencies.run,
		resolved.executablePath,
		"mcp", "remove", server.Name, "--scope", "user",
	)
	if err != nil {
		return RegistrationStateUnreadable, nil, err
	}
	return RegistrationStateNotConfigured, nil, nil
}
