// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file integrates VS Code through its user-level MCP configuration.
package mcpclientinstaller

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// VS Code introduced MCP server support in 1.99. Installation requires this
// version so VS Code can use the registration written to mcp.json.
const minimumVSCodeMCPVersion = "1.99.0"

type unsupportedClientVersionError struct {
	actual  string
	minimum string
}

func (e *unsupportedClientVersionError) Error() string {
	return fmt.Sprintf(
		"VS Code version %s does not support MCP servers; version %s or later is required",
		e.actual,
		e.minimum,
	)
}

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
				"bin",
				"code.cmd",
			),
			filepath.Join(
				dependencies.getenv("ProgramFiles"),
				"Microsoft VS Code",
				"bin",
				"code.cmd",
			),
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
	resolved, err := detectExecutableWithVersionValidator(
		ctx,
		c.dependencies,
		[]string{"code"},
		vscodeExecutablePaths(c.dependencies),
		"",
		true,
		validateVSCodeVersion,
	)
	resolved.configurationPath = c.configPath
	return resolved, err
}

func (*vscodeClient) RegistrationState(
	_ context.Context,
	server ServerDefinition,
	resolved resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	return readVSCodeJSONCRegistrationState(resolved.configurationPath, server)
}

func (c *vscodeClient) Install(
	ctx context.Context,
	server ServerDefinition,
	resolved resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	if err := installVSCodeJSONC(resolved.configurationPath, server); err != nil {
		return RegistrationStateUnreadable, nil, configurationWriteError(err)
	}
	return c.RegistrationState(ctx, server, resolved)
}

func (c *vscodeClient) Uninstall(
	ctx context.Context,
	server ServerDefinition,
	resolved resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	// Remove only the exact entry from the user mcp.json file.
	if err := uninstallVSCodeJSONC(resolved.configurationPath, server); err != nil {
		return RegistrationStateUnreadable, nil, configurationWriteError(err)
	}
	return c.RegistrationState(ctx, server, resolved)
}

func validateVSCodeVersion(output string) error {
	actual, version, err := parseVSCodeVersion(output)
	if err != nil {
		return err
	}
	minimum := [3]int{1, 99, 0}
	for index := range version {
		if version[index] > minimum[index] {
			return nil
		}
		if version[index] < minimum[index] {
			return &unsupportedClientVersionError{
				actual:  actual,
				minimum: minimumVSCodeMCPVersion,
			}
		}
	}
	return nil
}

func parseVSCodeVersion(output string) (string, [3]int, error) {
	// The launcher reports the version before the commit and architecture.
	// Insider releases may add a suffix; their numeric version determines support.
	firstLine, _, _ := strings.Cut(strings.TrimSpace(output), "\n")
	actual := strings.TrimSpace(firstLine)
	core, _, _ := strings.Cut(actual, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return actual, [3]int{}, fmt.Errorf("VS Code returned invalid version %q", actual)
	}
	var version [3]int
	for index, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			return actual, [3]int{}, fmt.Errorf("VS Code returned invalid version %q", actual)
		}
		version[index] = value
	}
	return actual, version, nil
}
