// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// source_sharing.go handles acknowledgements for MCP tools that may add source
// code and profiling data to a model's context. It prompts clients via
// form elicitation and saves the acknowledgment to the user's configuration
// directory for future calls.

package toolimpl

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
)

const mcpAcknowledgmentsDirname = "mcp-acknowledgments"
const sourceSharingAcknowledgementFilename = "source-code-sharing"
const sourceSharingKey = "source_code_sharing"

var sourceSharingMessage = "Some " + terminology.GetProductFullName() + " MCP tools may add portions of your source code " +
	"and related profiling data to your model's context. Do you allow this? If accepted, your acknowledgement " +
	"will be saved locally and apply to future conversations, including with other MCP clients, language models, " +
	"or model providers."

var errSourceSharingPermissionDenied = errors.New(
	"permission denied: the user did not approve source code sharing. " +
		"Tell the user that the request was not completed because permission was denied. ",
)

// checkSourceSharingAcknowledgement prompts form-capable clients unless this server
// or its saved configuration already has an acknowledgement. Clients without form
// elicitation support proceed without prompting or accessing acknowledgement storage.
func checkSourceSharingAcknowledgement(req *mcp.CallToolRequest, acknowledgedForProcess bool) (*mcp.CallToolResult, error) {
	if acknowledgedForProcess {
		return nil, nil
	}
	caps := req.ClientCapabilities()
	if caps == nil || caps.Elicitation == nil {
		return nil, nil
	}
	// An empty elicitation capability supports forms. URL-only clients do not.
	if caps.Elicitation.Form == nil && caps.Elicitation.URL != nil {
		return nil, nil
	}
	response, present := req.Params.InputResponses[sourceSharingKey]
	if present {
		// An explicit rejection should always block this call.
		result, ok := response.(*mcp.ElicitResult)
		if !ok || result.Action != "accept" || result.Content["acknowledged"] != true {
			return nil, errSourceSharingPermissionDenied
		}
	}

	if dir, err := userdirs.ConfigDir(); err == nil {
		filename := filepath.Join(dir, mcpAcknowledgmentsDirname, sourceSharingAcknowledgementFilename)
		if sourceSharingAcknowledged(filename) {
			return nil, nil
		}
		if present {
			// The user approved this call even if the acknowledgement cannot be saved.
			saveSourceSharingAcknowledgement(filename)
			return nil, nil
		}
	}
	if present {
		return nil, nil
	}
	// The SDK handles the prompt and retry for both current and older clients.
	return &mcp.CallToolResult{InputRequests: mcp.InputRequestMap{
		sourceSharingKey: &mcp.ElicitParams{
			Mode:    "form",
			Message: sourceSharingMessage,
			RequestedSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"acknowledged": {Type: "boolean", Title: "I acknowledge that source code and profiling data may be shared"},
				},
				Required: []string{"acknowledged"},
			},
		},
	}}, nil
}

func saveSourceSharingAcknowledgement(filename string) {
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return
	}
	// Exclusive creation prevents a raced-in symlink from being followed.
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return
	}
	_ = file.Close()
}

func sourceSharingAcknowledged(filename string) bool {
	_, err := os.Lstat(filename)
	return err == nil
}
