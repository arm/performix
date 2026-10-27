// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

type ImportRunTool struct{}

type importRunInput struct {
	Path string `json:"path"`
}

type importRunResult struct {
	RunID string     `json:"run_id,omitempty"`
	Error *toolError `json:"error,omitempty"`
}

var importRunInputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"path"},
	Properties: map[string]*jsonschema.Schema{
		"path": {Type: "string", Description: "Absolute path to a compatible Performix run .zip archive on the engine host."},
	},
}

var importRunOutputSchema = &jsonschema.Schema{
	Type: "object",
	OneOf: []*jsonschema.Schema{
		{Required: []string{"run_id"}},
		{Required: []string{"error"}},
	},
	Properties: map[string]*jsonschema.Schema{
		"run_id": {Type: "string", Description: "Imported run ID, returned on success."},
		"error":  toolErrorSchema(),
	},
}

func (ImportRunTool) Register(server *mcp.Server, toolDeps ToolDependencies) {
	mcp.AddTool(server, &mcp.Tool{
		Name:         "import_run",
		Description:  "Imports a local Performix run .zip archive and returns its run_id. If its ID already exists, the engine assigns a new ID without replacing the existing run.",
		Annotations:  &mcp.ToolAnnotations{DestructiveHint: jsonschema.Ptr(false)},
		InputSchema:  importRunInputSchema,
		OutputSchema: importRunOutputSchema,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input importRunInput) (*mcp.CallToolResult, importRunResult, error) {
		if strings.TrimSpace(input.Path) == "" {
			return importRunError(errors.New("path is required"))
		}
		if !filepath.IsAbs(input.Path) {
			return importRunError(errors.New("path must be an absolute path"))
		}
		path := filepath.Clean(input.Path)
		response, err := toolDeps.Engine.ImportRun(ctx, &apapproto.RunImportRequest{ExternalRunPath: path})
		if err != nil {
			return importRunError(err)
		}
		if response.GetReturnCode() != apapproto.StatusCode_SUCCESS {
			return importRunError(errors.New("engine failed to import run"))
		}
		return nil, importRunResult{RunID: response.GetNewId().GetValue()}, nil
	})
}

func importRunError(err error) (*mcp.CallToolResult, importRunResult, error) {
	return &mcp.CallToolResult{IsError: true}, importRunResult{Error: newToolError(err)}, nil
}
