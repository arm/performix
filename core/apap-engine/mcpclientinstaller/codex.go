// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file integrates Codex through its MCP CLI rather than editing Codex
// configuration files owned by the client.
package mcpclientinstaller

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
)

func codexPaths(dependencies clientDependencies) []string {
	// Codex is normally discovered on PATH. The official Windows installer also
	// creates a stable launcher under LOCALAPPDATA; npm installations use the
	// standard global npm bin directory. The MCP command owns configuration.
	// https://chatgpt.com/codex/install.ps1
	if dependencies.goos == "windows" {
		return []string{
			filepath.Join(
				dependencies.getenv("LOCALAPPDATA"),
				"Programs",
				"OpenAI",
				"Codex",
				"bin",
				"codex.exe",
			),
			filepath.Join(dependencies.getenv("APPDATA"), "npm", "codex.cmd"),
		}
	}
	return []string{
		filepath.Join(dependencies.home, ".local", "bin", "codex"),
		filepath.Join(dependencies.home, ".npm-global", "bin", "codex"),
		"/usr/local/bin/codex",
		"/opt/homebrew/bin/codex",
	}
}

type codexClient struct {
	dependencies clientDependencies
}

var _ client = (*codexClient)(nil)

func newCodexClient(dependencies clientDependencies) *codexClient {
	return &codexClient{dependencies: dependencies}
}

func (c *codexClient) Discovery() ClientDiscovery {
	return ClientDiscovery{Commands: []string{"codex"}, Paths: codexPaths(c.dependencies)}
}

func (c *codexClient) Detect(ctx context.Context) (resolvedClient, error) {
	return detectExecutable(
		ctx,
		c.dependencies,
		[]string{"codex"},
		codexPaths(c.dependencies),
		"codex",
		true,
	)
}

type codexServerConfiguration struct {
	Transport struct {
		Type    string
		Command string
		Args    []string
		Env     map[string]string
	} `json:"transport"`
}

func (c *codexClient) RegistrationState(
	ctx context.Context,
	server ServerDefinition,
	resolved resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	if !resolved.detected {
		return RegistrationStateNotConfigured, nil, nil
	}

	// Codex owns its TOML schema, so use its JSON status command instead of
	// parsing or rewriting config.toml ourselves.
	output, err := c.dependencies.run(
		ctx,
		resolved.executablePath,
		"mcp", "get", server.Name, "--json",
	)
	if err != nil {
		if containsAbsent(output) {
			return RegistrationStateNotConfigured, nil, nil
		}
		return RegistrationStateUnreadable, nil, fmt.Errorf("%w: %s", err, output)
	}

	var configuration codexServerConfiguration
	if err := json.Unmarshal(output, &configuration); err != nil {
		return RegistrationStateUnreadable, nil, err
	}
	transport := configuration.Transport
	differences := registrationDifferences(
		transport.Type,
		transport.Command,
		transport.Args,
		transport.Env,
		server,
		true,
	)
	if len(differences) == 0 {
		return RegistrationStateConfigured, nil, nil
	}
	return RegistrationStateConflict, differences, nil
}

func (c *codexClient) Install(
	ctx context.Context,
	server ServerDefinition,
	resolved resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	args := []string{"mcp", "add"}
	args = append(args, server.Name, "--", server.Command)
	args = append(args, server.Args...)
	if err := runClientCommand(ctx, c.dependencies.run, resolved.executablePath, args...); err != nil {
		return RegistrationStateUnreadable, nil, err
	}
	return c.RegistrationState(ctx, server, resolved)
}

func (c *codexClient) Uninstall(
	ctx context.Context,
	server ServerDefinition,
	resolved resolvedClient,
) (RegistrationState, []RegistrationDifference, error) {
	err := runClientCommand(
		ctx,
		c.dependencies.run,
		resolved.executablePath,
		"mcp", "remove", server.Name,
	)
	if err != nil {
		return RegistrationStateUnreadable, nil, err
	}
	return c.RegistrationState(ctx, server, resolved)
}
