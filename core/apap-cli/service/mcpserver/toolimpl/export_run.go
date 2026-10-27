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

type ExportRunTool struct{}

type exportRunInput struct {
	RunID           string `json:"run_id"`
	TargetDirectory string `json:"target_directory"`
}

type exportRunResult struct {
	ArchivePath string     `json:"archive_path,omitempty"`
	Error       *toolError `json:"error,omitempty"`
}

var exportRunInputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"run_id", "target_directory"},
	Properties: map[string]*jsonschema.Schema{
		"run_id":           {Type: "string", Description: "ID of the existing Performix run to export."},
		"target_directory": {Type: "string", Description: "Absolute destination directory on the engine host. Missing directories are created."},
	},
}

var exportRunOutputSchema = &jsonschema.Schema{
	Type: "object",
	OneOf: []*jsonschema.Schema{
		{Required: []string{"archive_path"}},
		{Required: []string{"error"}},
	},
	Properties: map[string]*jsonschema.Schema{
		"archive_path": {Type: "string", Description: "Absolute path of the exported <run_id>.zip archive on the engine host, returned on success."},
		"error":        toolErrorSchema(),
	},
}

func (ExportRunTool) Register(server *mcp.Server, toolDeps ToolDependencies) {
	mcp.AddTool(server, &mcp.Tool{
		Name:         "export_run",
		Description:  "Exports an existing Performix run to a host directory and returns the archive path.",
		InputSchema:  exportRunInputSchema,
		OutputSchema: exportRunOutputSchema,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input exportRunInput) (*mcp.CallToolResult, exportRunResult, error) {
		runID := strings.TrimSpace(input.RunID)
		if runID == "" {
			return exportRunError(errors.New("run_id is required"))
		}
		if strings.TrimSpace(input.TargetDirectory) == "" {
			return exportRunError(errors.New("target_directory is required"))
		}
		if !filepath.IsAbs(input.TargetDirectory) {
			return exportRunError(errors.New("target_directory must be an absolute path"))
		}
		directory := filepath.Clean(input.TargetDirectory)
		response, err := toolDeps.Engine.ExportRun(ctx, &apapproto.RunExportRequest{
			RunId: &apapproto.RunId{Value: runID}, TargetDirectory: directory,
		})
		if err != nil {
			return exportRunError(err)
		}
		if response.GetReturnCode() != apapproto.StatusCode_SUCCESS {
			return exportRunError(errors.New("engine failed to export run"))
		}
		return nil, exportRunResult{ArchivePath: filepath.Join(directory, runID+".zip")}, nil
	})
}

func exportRunError(err error) (*mcp.CallToolResult, exportRunResult, error) {
	return &mcp.CallToolResult{IsError: true}, exportRunResult{Error: newToolError(err)}, nil
}
