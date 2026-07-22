// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"fmt"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
)

// InitializeRunLog creates a DeferredFileOpenLogHook and attaches it to a logx logger. The supplied context is wrapped
// in a new context, inside which the logx logger is stashed. When using logx.FromContext() on the returned Context,
// logs written will write into the run log as well as into the main engine log. Once initialized, run logs will write
// into an in-memory buffer, until SwapToFile is called on the returned logging.DeferredFileOpenLogHook, at which point
// the buffered logs and all subsequent logs will be written into the file.
func InitializeRunLog(ctx context.Context) (*logging.DeferredFileOpenLogHook, context.Context) {
	// Set up log replication / hooking and attach context-aware logger.
	// Note: Swapping to log file will happen later.
	hook := logx.NewFileReplicationHook(&logging.JSONLinesFormatter{})
	runLogger := logx.HookAwareLogger(hook)
	return hook, logx.CtxWithLogger(ctx, runLogger)
}

func beginRunLogging(ctx recipe.ExecutionContext, logHook *logging.DeferredFileOpenLogHook) error {
	logFilename, err := ctx.StoreComponent("log.json", cdf.ComponentType{
		Name:          cdf.TypeLogJSON,
		SchemaVersion: "0.1",
	})
	if err != nil {
		return fmt.Errorf("failed to begin logging: %w", err)
	}

	if err := logHook.SwapToFile(logFilename); err != nil {
		return fmt.Errorf("failed to begin logging: %w", err)
	}

	return nil
}
