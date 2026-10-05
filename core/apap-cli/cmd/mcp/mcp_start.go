// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

type MCPRunner interface {
	Run(ctx context.Context, in io.ReadCloser, out io.Writer, errOut io.Writer) error
}

var errMCPShutdownSignal = errors.New("MCP shutdown signal received")

// signalCauseContext records when a signal cancels the command. This lets
// shutdown distinguish a signal from stdin closure or caller cancellation.
func signalCauseContext(parent context.Context) (context.Context, func()) {
	// Ignore SIGPIPE so a closed MCP client pipe cannot terminate apx before
	// the MCP server returns and shuts down its engine daemon. Reads from a
	// closed pipe still return EOF, while writes return EPIPE instead of
	// causing the Go process to exit with SIGPIPE.
	signal.Ignore(syscall.SIGPIPE)

	ctx, cancel := context.WithCancelCause(parent)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case received := <-signals:
			log.WithFields(log.Fields{
				"component": "mcp",
				"pid":       os.Getpid(),
				"ppid":      os.Getppid(),
				"signal":    received.String(),
			}).Debug("MCP server received shutdown signal")
			cancel(errMCPShutdownSignal)
		case <-ctx.Done():
		}
	}()
	return ctx, func() {
		signal.Stop(signals)
		cancel(context.Canceled)
	}
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
			// Signals cancel the same context used by stdin and caller cancellation,
			// so all termination paths run the engine shutdown sequence.
			ctx, stop := signalCauseContext(cmd.Context())
			defer stop()

			err := runner.Run(ctx, readCloser(cmd.InOrStdin()), cmd.OutOrStdout(), cmd.ErrOrStderr())
			if err != nil {
				wrappedErr := fmt.Errorf("failed to start MCP server: %w", err)
				clijson.HandleCLIError(cmd.ErrOrStderr(), wrappedErr)
				return clijson.MarkErrorHandled(wrappedErr)
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
