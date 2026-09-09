// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// mcp-client-stub implements the small native MCP command surface exercised by
// the Robot client-installation suite. The executable is copied under each
// client's command name so the engine discovers and invokes it normally.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type serverDefinition struct {
	Type    string   `json:"type,omitempty"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

var clientName string

func main() {
	clientName = strings.TrimSuffix(strings.ToLower(filepath.Base(os.Args[0])), ".exe")
	var err error
	switch clientName {
	case "claude":
		err = runClaude(os.Args[1:])
	case "codex":
		err = runCodex(os.Args[1:])
	case "code":
		err = runVSCode(os.Args[1:])
	default:
		err = fmt.Errorf("unsupported stub client %q", clientName)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runClaude(args []string) error {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println("Claude Code 1.0")
		return nil
	}
	if len(args) < 3 || args[0] != "mcp" {
		return fmt.Errorf("unsupported Claude Code arguments: %v", args)
	}
	switch args[1] {
	case "get":
		definition, err := readServer(args[2])
		if err != nil {
			return err
		}
		fmt.Printf(
			"%s:\n  Type: stdio\n  Command: %s\n  Args: %s\n",
			args[2],
			definition.Command,
			strings.Join(definition.Args, " "),
		)
		return nil
	case "add":
		name, definition, err := definitionFromAdd(args)
		if err != nil {
			return err
		}
		return writeServer(name, definition)
	case "remove":
		return removeServer(args[2])
	default:
		return fmt.Errorf("unsupported Claude Code MCP operation %q", args[1])
	}
}

func runCodex(args []string) error {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println("codex-cli 1.0")
		return nil
	}
	if len(args) < 3 || args[0] != "mcp" {
		return fmt.Errorf("unsupported Codex arguments: %v", args)
	}
	switch args[1] {
	case "get":
		definition, err := readServer(args[2])
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"transport": map[string]any{
				"type": definition.Type, "command": definition.Command,
				"args": definition.Args, "env": nil,
			},
		})
	case "add":
		name, definition, err := definitionFromAdd(args)
		if err != nil {
			return err
		}
		return writeServer(name, definition)
	case "remove":
		return removeServer(args[2])
	default:
		return fmt.Errorf("unsupported Codex MCP operation %q", args[1])
	}
}

func runVSCode(args []string) error {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println("1.0")
		return nil
	}
	if len(args) != 2 || args[0] != "--add-mcp" {
		return fmt.Errorf("unsupported VS Code arguments: %v", args)
	}
	var request struct {
		Name string `json:"name"`
		serverDefinition
	}
	if err := json.Unmarshal([]byte(args[1]), &request); err != nil {
		return err
	}
	request.Type = "stdio"
	path := os.Getenv("APAP_MCP_STUB_VSCODE_CONFIG")
	if path == "" {
		return fmt.Errorf("APAP_MCP_STUB_VSCODE_CONFIG is not set")
	}
	return writeJSON(path, map[string]any{
		"servers": map[string]serverDefinition{request.Name: request.serverDefinition},
	})
}

func definitionFromAdd(args []string) (string, serverDefinition, error) {
	separator := -1
	for index, argument := range args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 3 || separator+1 >= len(args) {
		return "", serverDefinition{}, fmt.Errorf("invalid MCP add arguments: %v", args)
	}
	return args[separator-1], serverDefinition{
		Type: "stdio", Command: args[separator+1], Args: args[separator+2:],
	}, nil
}

func readServer(name string) (serverDefinition, error) {
	servers, err := readServers()
	if err != nil {
		return serverDefinition{}, err
	}
	definition, ok := servers[name]
	if !ok {
		return serverDefinition{}, fmt.Errorf("No MCP server found for %s", name)
	}
	return definition, nil
}

func writeServer(name string, definition serverDefinition) error {
	servers, err := readServers()
	if err != nil {
		return err
	}
	servers[name] = definition
	return writeJSON(statePath(), servers)
}

func removeServer(name string) error {
	servers, err := readServers()
	if err != nil {
		return err
	}
	delete(servers, name)
	return writeJSON(statePath(), servers)
}

func readServers() (map[string]serverDefinition, error) {
	path := statePath()
	if path == "" {
		return nil, fmt.Errorf("APAP_MCP_STUB_STATE is not set")
	}
	servers := map[string]serverDefinition{}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return servers, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &servers); err != nil {
		return nil, err
	}
	return servers, nil
}

func statePath() string {
	prefix := os.Getenv("APAP_MCP_STUB_STATE")
	if prefix == "" {
		return ""
	}
	return prefix + "-" + clientName + ".json"
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
