// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	targetservice "github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type AddTargetTool struct{}

const defaultAddTargetPort = 22

type addTargetInput struct {
	Host           string               `json:"host"`
	User           string               `json:"user"`
	Port           int                  `json:"port,omitempty"`
	PrivateKeyPath string               `json:"private_key_path,omitempty"`
	Name           string               `json:"name,omitempty"`
	SetDefault     bool                 `json:"set_default,omitempty"`
	Jumps          []addTargetJumpInput `json:"jumps,omitempty"`
}

// addTargetJumpInput describes a single intermediate jump node on the path
// to the target. Jumps are ordered from the user's machine towards the target.
type addTargetJumpInput struct {
	Host           string `json:"host"`
	User           string `json:"user,omitempty"`
	Port           int    `json:"port,omitempty"`
	PrivateKeyPath string `json:"private_key_path,omitempty"`
}

var addTargetInputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"host", "user"},
	Properties: map[string]*jsonschema.Schema{
		"host": {
			Type:        "string",
			Description: "Hostname or IP address of the target to add.",
		},
		"user": {
			Type:        "string",
			Description: "Username for the SSH connection to the target.",
		},
		"port": {
			Type:        "integer",
			Default:     json.RawMessage(strconv.Itoa(defaultAddTargetPort)),
			Minimum:     jsonschema.Ptr(1.0),
			Maximum:     jsonschema.Ptr(float64(math.MaxUint16)),
			Description: "Port for the SSH connection to the target.",
		},
		"private_key_path": {
			Type: "string",
			Description: "Authentication is SSH key-based. Set private_key_path to use a specific private key. " +
				"If omitted, will automatically attempt to use any passphrase-less keys detected in common SSH key locations. ",
		},
		"name": {
			Type: "string",
			Description: "A descriptive name for this target configuration. This will help you identify it later. " +
				"A unique name is generated if omitted.",
		},
		"set_default": {
			Type: "boolean",
			Description: "Set set_default to true to make this new target the default target, " +
				"but only do so if the user explicitly requests this. " +
				"The first target added (via any route, not just MCP) may automatically be promoted to default.",
		},
		"jumps": {
			Type:        "array",
			Description: "Optional ordered list of intermediate jump (bastion/proxy) hosts to connect through to reach the target. Ordered from the user's machine towards the target.",
			Items: &jsonschema.Schema{
				Type:     "object",
				Required: []string{"host"},
				Properties: map[string]*jsonschema.Schema{
					"host": {
						Type:        "string",
						Description: "Hostname or IP address of the jump host.",
					},
					"user": {
						Type: "string",
						Description: "Use jumps to connect through one or more intermediate jump (bastion/proxy) hosts." +
							"List them in order from the user's machine towards the target, " +
							"each with its own host (required) and optional user, port, and private_key_path. ",
					},
					"port": {
						Type:        "integer",
						Default:     json.RawMessage(strconv.Itoa(defaultAddTargetPort)),
						Minimum:     jsonschema.Ptr(1.0),
						Maximum:     jsonschema.Ptr(float64(math.MaxUint16)),
						Description: "Port for the SSH connection to the jump host.",
					},
					"private_key_path": {
						Type:        "string",
						Description: "Path to the SSH private key file used to authenticate with the jump host.",
					},
				},
			},
		},
	},
}

type addTargetOutput struct {
	TargetName     string     `json:"target_name,omitempty"`
	Host           string     `json:"host,omitempty"`
	IsDefault      *bool      `json:"is_default,omitempty"`
	ConnectivityOK *bool      `json:"connectivity_ok,omitempty"`
	Error          *toolError `json:"error,omitempty"`
}

var addTargetOutputSchema = &jsonschema.Schema{
	Type: "object",
	OneOf: []*jsonschema.Schema{
		{
			Type:        "object",
			Description: "Target was added and the connectivity check succeeded.",
			Required:    []string{"target_name", "host", "is_default", "connectivity_ok"},
			Properties: map[string]*jsonschema.Schema{
				"connectivity_ok": {Type: "boolean", Enum: []any{true}},
			},
			Not: &jsonschema.Schema{Required: []string{"error"}},
		},
		{
			Type:        "object",
			Description: "Target was added but the connectivity check failed.",
			Required:    []string{"target_name", "host", "is_default", "connectivity_ok", "error"},
			Properties: map[string]*jsonschema.Schema{
				"connectivity_ok": {Type: "boolean", Enum: []any{false}},
			},
		},
		{
			Type:        "object",
			Description: "Either the target was not added, or there was an error after adding the target but before connectivity status could be reported. If target_name and host are present in the tool output, the target was persisted in the config.",
			Required:    []string{"error"},
			Not:         &jsonschema.Schema{Required: []string{"connectivity_ok"}},
		},
	},
	Properties: map[string]*jsonschema.Schema{
		"target_name": {
			Type:        "string",
			Description: "Friendly name of the target that was added, which may have been auto-generated.",
		},
		"host": {
			Type:        "string",
			Description: "Hostname or IP address of the target that was added.",
		},
		"is_default": {
			Type:        "boolean",
			Description: "True when the new target is the default. After adding the target, the persistent config is checked to accurately report whether the target is the default, as it may have been auto-promoted.",
		},
		"connectivity_ok": {
			Type:        "boolean",
			Description: "Result of the connectivity check performed after the target is added: true if the check succeeded, false if it failed.",
		},
		"error": toolErrorSchema(),
	},
}

// buildSSHTarget converts the validated tool input into an SSHTarget. Any jump hosts are
// prepended (in order, from the user's machine towards the target) ahead of the destination
// host in the Jumps slice, matching the ordering used by `apx target add --jump`. It
// validates each hop's port range before any persistence happens, returning a plain error
// for invalid input. The host key policy is always strict, so every hop's host key must
// already be trusted in the user's known_hosts file.
func buildSSHTarget(input addTargetInput) (*engine_target.SSHTarget, error) {
	jumps := make([]engine_target.SSHHostConfig, 0, len(input.Jumps)+1)

	for i, jump := range input.Jumps {
		hop, err := buildSSHHostConfig(jump.Host, jump.User, jump.Port, jump.PrivateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("jump host %d: %w", i+1, err)
		}
		jumps = append(jumps, hop)
	}

	destination, err := buildSSHHostConfig(input.Host, input.User, input.Port, input.PrivateKeyPath)
	if err != nil {
		return nil, err
	}
	jumps = append(jumps, destination)

	return &engine_target.SSHTarget{Jumps: jumps}, nil
}

// buildSSHHostConfig builds a single hop's SSHHostConfig, validating the port range before
// any persistence happens. The host key policy is always strict and authentication is
// always SSH key-based, matching the destination host.
func buildSSHHostConfig(host, username string, port int, privateKeyPath string) (engine_target.SSHHostConfig, error) {
	if port < 0 || port > math.MaxUint16 {
		return engine_target.SSHHostConfig{}, fmt.Errorf("invalid port %d: must be between 0 and %d", port, math.MaxUint16)
	}

	hostConfig := engine_target.SSHHostConfig{
		Host:               host,
		Port:               int32(port),
		Username:           username,
		PrivateKeyFilename: privateKeyPath,
		// Always require the host key to already be present in known_hosts (strict).
		HostKeyPolicy: engine_target.RejectHostKeyIfMissing,
		AuthMethod:    engine_target.SSHAuthMethodKey,
	}
	hostConfig.ApplyDefaults()

	return hostConfig, nil
}

// resolveTargetName returns the caller-supplied name, or generates a unique friendly name
// when none was provided. It only reads the target configuration when a name needs to be
// generated, so callers that supply a name do not depend on ReadTargetConfig.
func resolveTargetName(input addTargetInput, targets engine_target.TargetManagerService) (string, error) {
	if input.Name != "" {
		return input.Name, nil
	}

	return targetservice.GenerateUniqueTargetName(targets)
}

// testTargetConnectivity probes whether the engine can establish a connection to the
// target, mirroring `apx target test`. A failed probe does not roll back the target, but it
// is still surfaced as a tool execution error so MCP clients can react to it. It returns
// whether the connection succeeded and, on failure, structured error detail.
func testTargetConnectivity(ctx context.Context, engine apapproto.ApapClient, tgt engine_target.Target) (bool, *toolError) {
	response, err := (&targetservice.ConcreteTargetTester{}).TestTarget(ctx, engine, tgt)
	if err != nil {
		return false, newToolError(err)
	}

	if response.ConnectionStatus.ConnectionStatus != apapproto.ConnectionStatus_CONNECTION_STATUS_OK {
		if response.ConnectionStatus.Error != nil {
			return false, newToolError(response.ConnectionStatus.Error)
		}
		return false, newToolError(errors.New("connection to target failed"))
	}

	return true, nil
}

func (AddTargetTool) Register(server *mcp.Server, toolDeps ToolDependencies) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "add_target",
		Description: "Adds a new " + terminology.GetProductFullName() + " SSH target to the persistent target configuration so it can be used for future runs. " +
			"After adding the target, a connectivity check is performed. The target is still added even if this check fails. " +
			"Limitation of this tool: strict host key checking is enforced, so the target's SSH host key must already be present in the user's known_hosts file before connecting to the target. ",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: false,
		},
		InputSchema:  addTargetInputSchema,
		OutputSchema: addTargetOutputSchema,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input addTargetInput) (*mcp.CallToolResult, addTargetOutput, error) {
		tgt, err := buildSSHTarget(input)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, addTargetOutput{Error: newToolError(err)}, nil
		}

		name, err := resolveTargetName(input, toolDeps.Targets)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, addTargetOutput{Error: newToolError(err)}, nil
		}

		if err := toolDeps.Targets.AddTarget(name, tgt); err != nil {
			return &mcp.CallToolResult{IsError: true}, addTargetOutput{Error: newToolError(err)}, nil
		}

		// Implicitly false by default if not set
		// i.e. If there is already a non-localhost default target, don't make the new target the default unless user explicitly requests it
		if input.SetDefault {
			if err := toolDeps.Targets.SetDefaultTarget(name); err != nil {
				return &mcp.CallToolResult{IsError: true}, addTargetOutput{
					TargetName: name,
					Host:       input.Host,
					Error:      newToolError(err),
				}, nil
			}
		}

		// The target manager's AddTarget may promote the new target to default on its own (e.g. when
		// there is already a non-localhost default target), matching the CLI behaviour.
		// Read the persisted config back to accurately report whether the new target became the default.
		config, err := toolDeps.Targets.ReadTargetConfig()
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, addTargetOutput{
				TargetName: name,
				Host:       input.Host,
				Error:      newToolError(err),
			}, nil
		}

		// Probe connectivity after the target is persisted. A failed probe is reported as a
		// tool error but does not roll back the add, since the configuration may still be correct
		// (for example, the host key just needs to be trusted before the first run).
		connectivityOK, connectivityErr := testTargetConnectivity(ctx, toolDeps.Engine, tgt)

		isDefault := config.Default == name
		result := addTargetOutput{
			TargetName:     name,
			Host:           input.Host,
			IsDefault:      &isDefault,
			ConnectivityOK: &connectivityOK,
			Error:          connectivityErr,
		}
		if connectivityErr != nil {
			return &mcp.CallToolResult{IsError: true}, result, nil
		}
		return nil, result, nil
	})
}
