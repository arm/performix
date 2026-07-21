// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

// ComponentStore is the minimal interface needed to store run components - done to aid unit testing.
type ComponentStore interface {
	StoreComponent(dst string, componentType cdf.ComponentType) (string, error)
}

// UserMessageWriter writes user messages during a run.
type UserMessageWriter interface {
	Write(level, message string)
}

// ConcreteUserMessageWriter is the concrete implementation of UserMessageWriter.
type ConcreteUserMessageWriter struct {
	logger log.FieldLogger
}

// Open creates the underlying run componenet to store user messages and prepares the logger.
func (u *ConcreteUserMessageWriter) Open(store ComponentStore) (io.Closer, error) {
	componentPath := filepath.Join("user_messages", "user_messages.json")
	path, err := store.StoreComponent(componentPath,
		cdf.ComponentType{
			Name:          cdf.TypeLogJSON,
			SchemaVersion: "0.1",
		})
	if err != nil {
		return nil, message.New(message.EngineRunUserMessageOpenFailed).WithCause(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, perms.LocalFilePerm)
	if err != nil {
		return nil, message.New(message.EngineRunUserMessageOpenFailed).WithCause(err)
	}
	u.logger = &log.Logger{
		Out:       f,
		Formatter: &logging.JSONLinesFormatter{},
		Level:     log.InfoLevel,
	}
	return f, nil
}

// Write appends a user message at the specified level to the user message component.
func (u *ConcreteUserMessageWriter) Write(level, message string) {
	if u.logger == nil {
		return
	}
	switch strings.ToLower(level) {
	case "info":
		u.logger.Info(message)
	case "warn":
		u.logger.Warn(message)
	default:
		u.logger.Error(message)
	}
}
