// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file runs one or more MCP client operations concurrently and renders
// their progress and results in a stable order.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type operationFunc func(
	context.Context,
	apapproto.ApapClient,
	string,
) (operationResponse, error)

type operationResponse struct {
	status  *apapproto.MCPClientStatus
	outcome string
}

// runMCPOperations starts every selected client independently. It retains the
// requested order for completed output, even when one native client command is
// slower than another.
func runMCPOperations(
	cmd *cobra.Command,
	engineClient apapproto.ApapClient,
	ids []string,
	operation operationFunc,
	progressMessageCode message.MessageCode,
) error {
	type operationResult struct {
		output mcpClientOperationOutput
		err    error
	}

	progressWriter, progressTrackers, progressDone, err := startMCPProgress(
		cmd,
		ids,
		progressMessageCode,
	)
	if err != nil {
		return err
	}

	results := make([]operationResult, len(ids))
	var waitGroup sync.WaitGroup
	for index, id := range ids {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			response, err := operation(cmd.Context(), engineClient, id)
			if err != nil {
				markMCPProgressComplete(progressTrackers, index, true)
				results[index] = operationResult{
					output: mcpClientOperationOutput{
						Client: mcpClientOutput{
							ID:    id,
							Error: clijson.ExtractGRPCMessage(err),
						},
					},
					err: err,
				}
				return
			}
			markMCPProgressComplete(progressTrackers, index, false)
			results[index] = operationResult{
				output: mcpClientOperationOutput{
					Client:  clientOutput(response.status),
					Outcome: response.outcome,
				},
			}
		}()
	}
	waitGroup.Wait()
	stopMCPProgress(progressWriter, progressDone)

	outputs := make([]mcpClientOperationOutput, 0, len(ids))
	operationErrors := make([]error, 0)
	for _, result := range results {
		outputs = append(outputs, result.output)
		if result.err != nil {
			operationErrors = append(operationErrors, result.err)
		}
	}
	operationErr := errors.Join(operationErrors...)
	if len(operationErrors) == 1 {
		operationErr = operationErrors[0]
	}
	if viper.GetBool("json") {
		if err := clijson.MarshalJSONCLIResponseWithError(
			cmd.OutOrStdout(),
			outputs,
			operationErr,
		); err != nil {
			return err
		}
		if operationErr != nil {
			return clijson.MarkErrorHandled(operationErr)
		}
	}
	if operationErr == nil {
		return nil
	}

	for _, result := range results {
		if result.err == nil {
			continue
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s:\n", result.output.Client.ID)
		writeMCPErrorWithDetailsAndIndent(cmd.OutOrStdout(), result.err, 2)
		writeMCPCommandFailureDetails(cmd.OutOrStdout(), result.err)
		fmt.Fprintln(cmd.OutOrStdout())
	}
	return clijson.MarkErrorHandled(operationErr)
}

// writeMCPCommandFailureDetails preserves the native client's diagnostics. The
// installer catalogue explains the operation; the client output explains why
// that client's command failed.
func writeMCPCommandFailureDetails(out io.Writer, err error) {
	msg := message.IsMessage(err)
	if msg == nil || msg.Code() != message.EngineMcpRegistrationClientCommandFailed {
		return
	}
	writeMCPRawErrorDetail(
		out,
		message.EngineMcpRegistrationClientCommandErrorLabel,
		msg.Metadata()["commandError"],
	)
	writeMCPRawErrorDetail(
		out,
		message.EngineMcpRegistrationClientCommandOutputLabel,
		msg.Metadata()["commandOutput"],
	)
}

func writeMCPRawErrorDetail(out io.Writer, labelCode message.MessageCode, detail string) {
	if detail == "" {
		return
	}
	label, err := message.LookupMessage(message.New(labelCode))
	if err == nil {
		fmt.Fprintf(out, "  %s\n", label.Message)
	}
	fmt.Fprint(out, detail)
	if !strings.HasSuffix(detail, "\n") {
		fmt.Fprintln(out)
	}
}
