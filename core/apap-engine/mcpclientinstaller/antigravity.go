// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file manages Antigravity's documented global MCP configuration file.
package mcpclientinstaller

import (
	"context"
	"path/filepath"
)

type antigravityClient struct {
	dependencies     clientDependencies
	configPath       string
	applicationPaths []string
}

var _ client = (*antigravityClient)(nil)

func newAntigravityClient(dependencies clientDependencies) *antigravityClient {
	return &antigravityClient{
		dependencies:     dependencies,
		configPath:       filepath.Join(dependencies.home, ".gemini", "config", "mcp_config.json"),
		applicationPaths: antigravityApplicationPaths(dependencies),
	}
}

func antigravityApplicationPaths(dependencies clientDependencies) []string {
	// Antigravity documents ~/.gemini/config/mcp_config.json as the global MCP
	// configuration. Application paths are kept separate as installation
	// evidence, so a retained configuration directory alone is never detected.
	// https://antigravity.google/docs/mcp
	// The Windows CLI installer documents LOCALAPPDATA/agy/bin for agy.exe:
	// https://antigravity.google/docs/cli-install
	// The IDE candidates cover the current and legacy Windows package names:
	// https://antigravity.google/download
	switch dependencies.goos {
	case "darwin":
		return []string{
			"/Applications/Antigravity.app",
			filepath.Join(dependencies.home, "Applications", "Antigravity.app"),
		}
	case "windows":
		return []string{
			filepath.Join(dependencies.getenv("LOCALAPPDATA"), "agy", "bin", "agy.exe"),
			filepath.Join(
				dependencies.getenv("LOCALAPPDATA"),
				"Programs",
				"antigravity-ide",
				"Antigravity IDE.exe",
			),
			filepath.Join(
				dependencies.getenv("LOCALAPPDATA"),
				"Programs",
				"Antigravity",
				"Antigravity.exe",
			),
		}
	case "linux":
		return []string{
			filepath.Join(dependencies.home, ".gemini", "antigravity"),
		}
	default:
		return nil
	}
}

func (c *antigravityClient) Discovery() ClientDiscovery {
	commands := []string(nil)
	if c.dependencies.goos == "windows" {
		commands = []string{"agy"}
	}
	return ClientDiscovery{Commands: commands, Paths: c.applicationPaths}
}

func (c *antigravityClient) Detect(ctx context.Context) (resolvedClient, error) {
	// Antigravity may be installed without a shell launcher. The application
	// location is therefore the installation signal; MCP configuration is JSON.
	if c.dependencies.goos != "windows" {
		return resolvedClient{
			detected:          anyPathExists(c.dependencies, c.applicationPaths),
			configurationPath: c.configPath,
		}, nil
	}
	resolved, err := detectExecutable(
		ctx,
		c.dependencies,
		[]string{"agy"},
		c.applicationPaths,
		"",
		false,
	)
	resolved.executablePath = ""
	resolved.configurationPath = c.configPath
	return resolved, err
}

func (*antigravityClient) RegistrationState(
	_ context.Context,
	server ServerDefinition,
	resolved resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	return jsonRegistrationState(resolved.configurationPath, "mcpServers", server, false)
}

func (c *antigravityClient) Install(
	ctx context.Context,
	server ServerDefinition,
	resolved resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	if err := installJSON(resolved.configurationPath, "mcpServers", server, false); err != nil {
		return RegistrationStateUnreadable, nil, configurationWriteError(err)
	}
	return c.RegistrationState(ctx, server, resolved)
}

func (c *antigravityClient) Uninstall(
	ctx context.Context,
	server ServerDefinition,
	resolved resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	if err := uninstallJSON(resolved.configurationPath, "mcpServers", server.Name); err != nil {
		return RegistrationStateUnreadable, nil, configurationWriteError(err)
	}
	return c.RegistrationState(ctx, server, resolved)
}
