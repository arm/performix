// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

type MCPRunner interface {
	Run(ctx context.Context, in io.ReadCloser, out io.Writer, errOut io.Writer) error
}

func newMCPStartCmd(runner MCPRunner) *cobra.Command {

	cmd := &cobra.Command{
		Use:          "start",
		Short:        fmt.Sprintf("Start the %v MCP server.", terminology.GetProductFullName()),
		Long:         fmt.Sprintf("Start the %v Model Context Protocol (MCP) server, so that it's ready to use with an AI coding agent that supports MCP.", terminology.GetProductFullName()),
		Args:         cobra.ExactArgs(0),
		SilenceUsage: true,
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupMCPSub,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			err := runner.Run(cmd.Context(), readCloser(cmd.InOrStdin()), cmd.OutOrStdout(), cmd.ErrOrStderr())
			if err != nil {
				wrappedErr := fmt.Errorf("failed to start MCP server: %w", err)
				clijson.HandleCLIError(cmd.ErrOrStderr(), wrappedErr)
				return errors.Join(clijson.ErrorAlreadyHandled, wrappedErr)
			}

			return nil
		},
	}

	return cmd
}

func readCloser(in io.Reader) io.ReadCloser {
	if rc, ok := in.(io.ReadCloser); ok {
		return rc
	}
	return io.NopCloser(in)
}
