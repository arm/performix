// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file integrates VS Code through its documented --add-mcp CLI command.
package mcpclientinstaller

import (
	"context"
	"encoding/json"
	"path/filepath"
)

func vscodeExecutablePaths(dependencies clientDependencies) []string {
	// VS Code documents code as the command-line entry point, including the
	// packaged macOS launcher and standard Windows/Linux PATH installation.
	// https://code.visualstudio.com/docs/configure/command-line
	switch dependencies.goos {
	case "darwin":
		return []string{
			"/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code",
			filepath.Join(
				dependencies.home,
				"Applications",
				"Visual Studio Code.app",
				"Contents",
				"Resources",
				"app",
				"bin",
				"code",
			),
		}
	case "windows":
		return []string{
			filepath.Join(
				dependencies.getenv("LOCALAPPDATA"),
				"Programs",
				"Microsoft VS Code",
				"Code.exe",
			),
			filepath.Join(dependencies.getenv("ProgramFiles"), "Microsoft VS Code", "Code.exe"),
		}
	default:
		return []string{"/usr/bin/code", "/usr/local/bin/code", "/snap/bin/code"}
	}
}

func vscodeConfigPath(dependencies clientDependencies) string {
	switch dependencies.goos {
	case "darwin":
		return filepath.Join(
			dependencies.home,
			"Library",
			"Application Support",
			"Code",
			"User",
			"mcp.json",
		)
	case "windows":
		return filepath.Join(dependencies.getenv("APPDATA"), "Code", "User", "mcp.json")
	default:
		configHome := dependencies.getenv("XDG_CONFIG_HOME")
		if configHome == "" {
			configHome = filepath.Join(dependencies.home, ".config")
		}
		return filepath.Join(configHome, "Code", "User", "mcp.json")
	}
}

type vscodeClient struct {
	dependencies clientDependencies
	configPath   string
}

var _ client = (*vscodeClient)(nil)

func newVSCodeClient(dependencies clientDependencies) *vscodeClient {
	return &vscodeClient{
		dependencies: dependencies,
		configPath:   vscodeConfigPath(dependencies),
	}
}

func (c *vscodeClient) Discovery() ClientDiscovery {
	return ClientDiscovery{Commands: []string{"code"}, Paths: vscodeExecutablePaths(c.dependencies)}
}

func (c *vscodeClient) Detect(ctx context.Context) (resolvedClient, error) {
	// The `code` shell command is optional, but packaged installations include
	// the same launcher inside the application. The standard candidates cover
	// both forms.
	resolved, err := detectExecutable(
		ctx,
		c.dependencies,
		[]string{"code"},
		vscodeExecutablePaths(c.dependencies),
		"",
		true,
	)
	resolved.configurationPath = c.configPath
	return resolved, err
}

func (*vscodeClient) RegistrationState(
	_ context.Context,
	server ServerDefinition,
	resolved resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	return jsonRegistrationState(resolved.configurationPath, "servers", server, true)
}

func (c *vscodeClient) Install(
	ctx context.Context,
	server ServerDefinition,
	resolved resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	definition, err := json.Marshal(struct {
		Name    string   `json:"name"`
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}{
		Name:    server.Name,
		Command: server.Command,
		Args:    server.Args,
	})
	if err != nil {
		return RegistrationStateUnreadable, nil, err
	}
	if err := runClientCommand(
		ctx,
		c.dependencies.run,
		resolved.executablePath,
		"--add-mcp", string(definition),
	); err != nil {
		return RegistrationStateUnreadable, nil, err
	}
	return c.RegistrationState(ctx, server, resolved)
}

func (c *vscodeClient) Uninstall(
	ctx context.Context,
	server ServerDefinition,
	resolved resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	// VS Code provides --add-mcp but no corresponding non-interactive removal
	// command. Remove only the exact entry from its user mcp.json file.
	if err := uninstallJSON(resolved.configurationPath, "servers", server.Name); err != nil {
		return RegistrationStateUnreadable, nil, configurationWriteError(err)
	}
	return c.RegistrationState(ctx, server, resolved)
}
