// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package clijson

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/viper"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/grpc/status"

	"github.com/Arm-Debug/apap-cli/apap-cli/service"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

type CliJSONResponse[T any] struct {
	Code     string        `json:"code"`
	Error    *ErrorPayload `json:"error,omitempty"`
	Data     T             `json:"data"`
	GRPCInfo GRPCInfo      `json:"grpc_info,omitempty"`
}

type ErrorPayload = message.ErrorPayload

type GRPCInfo struct {
	GRPCCode    string `json:"grpc_code"`
	GRPCMessage string `json:"grpc_message"`
}

// Alias for the message lookup function to allow easier mocking in tests
var LookupMsg = message.LookupMessage

// BuildErrorTree builds a full ErrorPayload tree for JSON output.
// It recurses into all wrapped errors, looking up each node in the catalog
// if it is a MessageImpl. If the node is not a MessageImpl, it builds a plain
// error node with just the raw error string.
func BuildErrorTree(err error) *ErrorPayload {
	return message.BuildErrorPayload(err, &message.ErrorPayloadOptions{
		LookupMessage: LookupMsg,
		FormatNonMessage: func(err error) string {
			_, grpcMessage, grpcDetails, isGRPC := service.GRPCErrors{}.ExtractGRPCError(err)
			if isGRPC {
				return fmt.Sprintf("%s \n- gRPC Message: %s", grpcDetails, grpcMessage)
			}
			return err.Error()
		},
	})
}

// MarshalJSONCLIResponse outputs a data response
func MarshalJSONCLIResponse[T any](out io.Writer, data T) error {
	return MarshalJSONCLIResponseWithError(out, data, nil)
}

// ExtractGRPCMessage extracts only the error message from a gRPC status.
func ExtractGRPCMessage(err error) string {
	st, ok := status.FromError(err)
	if !ok {
		return err.Error()
	}
	return st.Message()
}

// MarshalJSONCLIResponseWithError outputs both result and an optional error
func MarshalJSONCLIResponseWithError[T any](out io.Writer, data T, err error) error {
	return MarshalJSONCLIResponseWithErrorAndSeverity(out, data, err, "")
}

// MarshalJSONCLIResponseWithErrorAndSeverity outputs both result and an optional error, with the severity set to the
// specified value
func MarshalJSONCLIResponseWithErrorAndSeverity[T any](out io.Writer, data T, err error, severity message.Severity) error {
	resp := CliJSONResponse[T]{
		Code:     "0",
		Error:    &ErrorPayload{},
		Data:     data,
		GRPCInfo: GRPCInfo{GRPCCode: code.Code_OK.String(), GRPCMessage: ""},
	}

	if err != nil {
		resp.Code = "-1"

		// If the top level error is a MessageImpl, extract gRPC info from it
		// For plain errors, we just cobble together the best we can and insert
		// it into the error payload message.
		if m, ok := err.(*message.MessageImpl); ok {
			resp.GRPCInfo.GRPCCode = m.GRPCCode()
			resp.GRPCInfo.GRPCMessage = m.GRPCMessage()
		}

		// Build the full error payload, which could be a tree containing any
		// combination of Messages and plain errors.
		if payload := BuildErrorTree(err); payload != nil {
			if severity != "" {
				payload.Severity = severity
			}
			resp.Error = payload
		}
	}

	// Marshal and write output
	jsonData, marshalErr := json.Marshal(resp)
	if marshalErr != nil {
		return message.New(message.CliCmdCommonJsonMarshalFailed).WithCause(marshalErr)
	}

	fmt.Fprintln(out, string(jsonData))
	return nil
}

// ErrorAlreadyHandled indicates that the error has already been handled by the CLI command.
// It is used to suppress error handling at the command root if the command needs more
// control over error reporting.
var ErrorAlreadyHandled = errors.New("CLI error already handled")

// HandleCLIError handles errors for CLI commands. Handling is different
// depending on whether the output should be in JSON format or text output.
func HandleCLIError(out io.Writer, err error) {
	if errors.Is(err, ErrorAlreadyHandled) {
		return
	}
	if viper.GetBool("json") {
		_ = MarshalJSONCLIResponseWithError(out, emptyStruct{}, err)
	} else {
		HandlePlaintextCLIErrorWithIndent(out, err, 0)
	}
}

// HandlePlaintextCLIErrorWithIndent handles errors for CLI commands, producing plaintext (non-JSON) output with
// the specified number of spaces before each line.
func HandlePlaintextCLIErrorWithIndent(out io.Writer, err error, numChars int) {
	if err == nil || errors.Is(err, ErrorAlreadyHandled) {
		return
	}

	// Look up the message in the catalog
	if catalogMsg, lookupErr := LookupMsg(err); lookupErr == nil {
		fmt.Fprintln(out, catalogMsg.StringWithIndent(numChars))
	} else {
		// Fallback to the raw error message if lookup fails
		fmt.Fprintf(out, "%v%v\n", strings.Repeat(" ", numChars), err.Error())
	}
}

// MarshalJSON implements a custom JSON marshaller for CLIRunDescription.
// It first encodes the struct with the default JSON rules (including the
// “renderer_output” map), then decodes that into a generic map[string]interface{}.
// Any entries under “renderer_output” are promoted to top‐level keys, and
// the “renderer_output” wrapper is removed. Finally, the modified map is
// re‐encoded as JSON bytes.
//
// This ensures that all dynamic tables in RendererOutput appear directly
// under the main object rather than inside a “renderer_output” field.
func (c CLIRunDescription) MarshalJSON() ([]byte, error) {
	// Create a local copy type so we can run the default JSON logic
	// without recursing back into this MarshalJSON method.
	type alias CLIRunDescription

	// Marshal the copy as if it were a plain struct.
	// This gives us a JSON blob that still contains "renderer_output".
	rawBytes, err := json.Marshal(alias(c))
	if err != nil {
		return nil, fmt.Errorf("failed to json.Marshal CLIRunDescription: %w", err)
	}

	var topMap map[string]interface{}
	if err := json.Unmarshal(rawBytes, &topMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal CLIRunDescription JSON into map: %w", err)
	}

	// Look for "renderer_output" in that map.
	if roAny, exists := topMap["renderer_output"]; exists {
		if roMap, ok := roAny.(map[string]interface{}); ok {
			for subKey, subVal := range roMap {
				topMap[subKey] = subVal
			}
		}
		// Remove the original "renderer_output" wrapper entirely.
		delete(topMap, "renderer_output")
	}

	// Convert back to JSON without the "renderer_output" object.
	return json.Marshal(topMap)
}

type emptyStruct struct {
}
