// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file manages Claude Desktop's MCP JSON configuration for detected GUI
// installations, without requiring the separate Claude Code CLI.
package mcpclientinstaller

import (
	"context"
	"path/filepath"
)

type claudeDesktopClient struct {
	dependencies     clientDependencies
	applicationPaths []string
}

var _ client = (*claudeDesktopClient)(nil)

func newClaudeDesktopClient(dependencies clientDependencies) *claudeDesktopClient {
	return &claudeDesktopClient{
		dependencies:     dependencies,
		applicationPaths: claudeDesktopApplicationPaths(dependencies),
	}
}

func claudeDesktopApplicationPaths(dependencies clientDependencies) []string {
	switch dependencies.goos {
	case "darwin":
		return []string{
			"/Applications/Claude.app",
			filepath.Join(dependencies.home, "Applications", "Claude.app"),
		}
	case "windows":
		return []string{
			// Anthropic distributes Claude Desktop as an MSIX package. Windows
			// removes its app execution alias when the package is uninstalled.
			// The Programs path covers the unpackaged per-user installer.
			// https://support.claude.com/en/articles/12622703-deploy-claude-desktop-for-windows
			filepath.Join(
				dependencies.getenv("LOCALAPPDATA"),
				"Microsoft",
				"WindowsApps",
				"claude.exe",
			),
			filepath.Join(
				dependencies.getenv("LOCALAPPDATA"),
				"Programs",
				"Claude",
				"Claude.exe",
			),
		}
	case "linux":
		// Anthropic's apt and deb packages install the documented
		// `claude-desktop` launcher.
		// https://support.claude.com/en/articles/10065433-install-claude-desktop
		return []string{"/usr/bin/claude-desktop", "/usr/local/bin/claude-desktop"}
	default:
		return nil
	}
}

func claudeDesktopConfigDirectories(dependencies clientDependencies) []string {
	// Claude Desktop uses the documented claude_desktop_config.json file.
	// https://docs.anthropic.com/en/docs/mcp
	switch dependencies.goos {
	case "darwin":
		return []string{
			filepath.Join(dependencies.home, "Library", "Application Support", "Claude"),
		}
	case "windows":
		return []string{filepath.Join(dependencies.getenv("APPDATA"), "Claude")}
	default:
		configHome := dependencies.getenv("XDG_CONFIG_HOME")
		if configHome == "" {
			configHome = filepath.Join(dependencies.home, ".config")
		}
		return []string{
			filepath.Join(configHome, "Claude"),
			filepath.Join(configHome, "claude"),
		}
	}
}

func claudeDesktopConfigPath(dependencies clientDependencies) string {
	for _, directory := range claudeDesktopConfigDirectories(dependencies) {
		if _, err := dependencies.stat(directory); err == nil {
			return filepath.Join(directory, "claude_desktop_config.json")
		}
	}
	return filepath.Join(
		claudeDesktopConfigDirectories(dependencies)[0],
		"claude_desktop_config.json",
	)
}

func (c *claudeDesktopClient) Discovery() ClientDiscovery {
	commands := []string(nil)
	if c.dependencies.goos == "linux" {
		commands = []string{"claude-desktop"}
	}
	return ClientDiscovery{Commands: commands, Paths: c.applicationPaths}
}

func (c *claudeDesktopClient) Detect(ctx context.Context) (resolvedClient, error) {
	// Claude Desktop does not require its optional CLI to manage MCP servers.
	// Detect its platform location and use the JSON configuration directly.
	if c.dependencies.goos == "linux" {
		resolved, err := detectExecutable(
			ctx,
			c.dependencies,
			[]string{"claude-desktop"},
			c.applicationPaths,
			"",
			false,
		)
		resolved.configurationPath = claudeDesktopConfigPath(c.dependencies)
		return resolved, err
	}
	return resolvedClient{
		detected:          anyPathExists(c.dependencies, c.applicationPaths),
		configurationPath: claudeDesktopConfigPath(c.dependencies),
	}, nil
}

func (*claudeDesktopClient) RegistrationState(
	_ context.Context,
	server ServerDefinition,
	resolved resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	return jsonRegistrationState(resolved.configurationPath, "mcpServers", server, false)
}

func (c *claudeDesktopClient) Install(
	ctx context.Context,
	server ServerDefinition,
	resolved resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	if err := installJSON(resolved.configurationPath, "mcpServers", server, false); err != nil {
		return RegistrationStateUnreadable, nil, configurationWriteError(err)
	}
	return c.RegistrationState(ctx, server, resolved)
}

func (c *claudeDesktopClient) Uninstall(
	ctx context.Context,
	server ServerDefinition,
	resolved resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	if err := uninstallJSON(resolved.configurationPath, "mcpServers", server.Name); err != nil {
		return RegistrationStateUnreadable, nil, configurationWriteError(err)
	}
	return c.RegistrationState(ctx, server, resolved)
}
