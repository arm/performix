// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package logx

import (
	"context"
	"sync/atomic"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/logging"
)

//
// logx provides structured, context-aware logging for services where
// logs may need to be replicated to additional destinations depending
// on the request context (e.g., writing logs for a "Run" into a dedicated
// run-specific file in addition to the global log).
//
// It builds on top of logrus and introduces conventions for safely
// using per-request loggers without needing to pass logger instances
// everywhere.
//
// # Core Concepts
//
// - The **global logger** is a singleton logger that should be used when
//   no context-specific logger is available.
// - A **context-scoped logger** can be attached to a `context.Context` and
//   retrieved anywhere downstream in the call stack.
// - A **log hook** (like `DeferredFileOpenLogHook`) can be used to replicate
//   log entries to additional destinations (e.g., per-request log files).
//
// # Conventions
//
// 1. Always use `logx.FromContext(ctx)` to retrieve a logger inside request
//    handlers or any downstream processing.
// 2. Use `logx.CtxWithLogger(ctx, logger)` to attach a scoped logger if you
//    are setting up a logger for a specific context (e.g., in a gRPC handler).
// 3. Do not use `logrus.StandardLogger()` or the global `logrus` functions
//    (`logrus.Infof`, etc.). These bypass context and may log to the wrong place.
// 4. Log only from components that can provide meaningful context. Avoid
//    excessive logging in utility code or deeply nested libraries unless
//    it is relevant to diagnosis or observability.
// 5. To replicate logs to additional files (e.g., per-Run logs), use
//    `logx.HookAwareLogger(...)` with a hook like `DeferredFileOpenLogHook`.
//
// # Example Usage
//
//     func HandleRun(ctx context.Context, runID string) error {
//         hook := logx.NewFileReplicationHook()
//         runLogger := logx.HookAwareLogger(hook)
//         ctx = logx.CtxWithLogger(ctx, runLogger)
//
//         logx.FromContext(ctx).Info("Starting run")
//
//         err := hook.SwapToFile("/path/to/run.log")
//         if err != nil {
//             logx.FromContext(ctx).WithError(err).Warn("Failed to initialize run log")
//         }
//
//         ...
//     }
//

// loggerKey is the context key used to store loggers
type loggerKey struct{}

// Global logger instance. Used when no context-scoped logger is available.
var (
	globalLogger atomic.Value = atomize(defaultLogger())
)

// defaultLogger returns the default logger instance
func defaultLogger() log.FieldLogger {
	return log.StandardLogger()
}

// wrap a value in an atomic
func atomize(v any) atomic.Value {
	var atom atomic.Value
	atom.Store(v)
	return atom
}

// SetGlobalLogger replaces the default global logger.
// This can be used in main() or tests to install a custom logger.
func SetGlobalLogger(l log.FieldLogger) {
	globalLogger.Store(l)
}

// GlobalLogger returns the currently configured global logger.
// This should only be used to seed context-scoped loggers or by logging infrastructure.
// Application code should prefer FromContext.
func GlobalLogger() log.FieldLogger {
	return globalLogger.Load().(log.FieldLogger)
}

// CtxWithLogger returns a copy of ctx with the given logger attached.
// Use this at the boundary of a new request or when constructing a
// request-scoped logger.
func CtxWithLogger(ctx context.Context, l log.FieldLogger) context.Context {
	return context.WithValue(ctx, loggerKey{}, l)
}

// FromContext retrieves a logger from context.
// Falls back to the global logger if no scoped logger is available.
// This should be the primary way to retrieve a logger inside application code.
func FromContext(ctx context.Context) log.FieldLogger {
	if ctx != nil {
		if l, ok := ctx.Value(loggerKey{}).(log.FieldLogger); ok && l != nil {
			return l
		}
	}
	return GlobalLogger()
}

// HookAwareLogger returns a new logrus.Logger configured with the
// same formatter, level, and output as the global logger, but with
// an additional hook attached.
//
// This is intended for constructing context-scoped loggers that write
// to additional destinations, such as request- or run-specific log files.
func HookAwareLogger(hook log.Hook) *log.Logger {
	l := log.New()
	glog := GlobalLogger()
	l.SetFormatter(glog.(*log.Logger).Formatter)
	l.SetLevel(glog.(*log.Logger).Level)
	l.SetOutput(glog.(*log.Logger).Out)

	l.AddHook(hook)
	return l
}

// NewFileReplicationHook creates a new log hook that writes to the given file path,
// using the same formatter as the global logger.
//
// This is a convenience wrapper around NewDeferredFileOpenLogHook.
func NewFileReplicationHook(formatter log.Formatter) *logging.DeferredFileOpenLogHook {
	if formatter == nil {
		formatter = GlobalLogger().(*log.Logger).Formatter
	}

	return logging.NewDeferredFileOpenLogHook(formatter)
}
