// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file manages Cursor's user MCP configuration after detecting its GUI
// installation from platform application locations.
package mcpclientinstaller

import (
	"context"
	"path/filepath"
)

type cursorClient struct {
	dependencies     clientDependencies
	configPath       string
	applicationPaths []string
}

var _ client = (*cursorClient)(nil)

func newCursorClient(dependencies clientDependencies) *cursorClient {
	return &cursorClient{
		dependencies:     dependencies,
		configPath:       filepath.Join(dependencies.home, ".cursor", "mcp.json"),
		applicationPaths: cursorApplicationPaths(dependencies),
	}
}

func cursorApplicationPaths(dependencies clientDependencies) []string {
	// Cursor documents ~/.cursor/mcp.json for MCP servers. Use the executable
	// installed by its Windows user or system installer as installation evidence,
	// rather than roaming application data which can survive removal.
	// https://docs.cursor.com/en/tools/mcp
	switch dependencies.goos {
	case "darwin":
		return []string{
			"/Applications/Cursor.app",
			filepath.Join(dependencies.home, "Applications", "Cursor.app"),
		}
	case "windows":
		return []string{
			filepath.Join(
				dependencies.getenv("LOCALAPPDATA"),
				"Programs",
				"cursor",
				"Cursor.exe",
			),
			filepath.Join(dependencies.getenv("ProgramFiles"), "Cursor", "Cursor.exe"),
		}
	case "linux":
		// Cursor's AppImage may be stored anywhere. The deb and rpm packages,
		// and AppImages with an installed shell launcher, expose `cursor` on
		// PATH. These standard launcher paths cover package installations
		// without mistaking retained application data for an installation.
		// https://docs.cursor.com/en/troubleshooting/troubleshooting-guide
		return []string{"/usr/bin/cursor", "/usr/local/bin/cursor"}
	default:
		return nil
	}
}

func (c *cursorClient) Discovery() ClientDiscovery {
	commands := []string(nil)
	if c.dependencies.goos == "linux" {
		commands = []string{"cursor"}
	}
	return ClientDiscovery{Commands: commands, Paths: c.applicationPaths}
}

func (c *cursorClient) Detect(ctx context.Context) (resolvedClient, error) {
	// Cursor's deep-link installer requires user interaction. Detect the desktop
	// application and manage its documented user configuration directly.
	if c.dependencies.goos == "linux" {
		resolved, err := detectExecutable(
			ctx,
			c.dependencies,
			[]string{"cursor"},
			c.applicationPaths,
			"",
			false,
		)
		resolved.configurationPath = c.configPath
		return resolved, err
	}
	for _, path := range c.applicationPaths {
		if _, err := c.dependencies.stat(path); err == nil {
			return resolvedClient{
				detected:          true,
				configurationPath: c.configPath,
			}, nil
		}
	}
	return resolvedClient{
		configurationPath: c.configPath,
	}, nil
}

func (*cursorClient) RegistrationState(
	_ context.Context,
	server ServerDefinition,
	resolved resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	return jsonRegistrationState(resolved.configurationPath, "mcpServers", server, false)
}

func (c *cursorClient) Install(
	ctx context.Context,
	server ServerDefinition,
	resolved resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	if err := installJSON(resolved.configurationPath, "mcpServers", server, false); err != nil {
		return RegistrationStateUnreadable, nil, configurationWriteError(err)
	}
	return c.RegistrationState(ctx, server, resolved)
}

func (c *cursorClient) Uninstall(
	ctx context.Context,
	server ServerDefinition,
	resolved resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	if err := uninstallJSON(resolved.configurationPath, "mcpServers", server.Name); err != nil {
		return RegistrationStateUnreadable, nil, configurationWriteError(err)
	}
	return c.RegistrationState(ctx, server, resolved)
}
