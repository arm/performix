// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestLogger wires the custom formatter to an in-memory buffer.
func newTestLogger(buf *bytes.Buffer) *logrus.Logger {
	l := logrus.New()
	l.SetFormatter(&JSONLinesFormatter{})
	l.SetOutput(buf)
	return l
}

// decode trims the trailing newline and unmarshals the JSON line.
func decode(line string) (map[string]any, error) {
	var m map[string]any
	err := json.Unmarshal([]byte(strings.TrimSuffix(line, "\n")), &m)
	return m, err
}

// ----------------------------------------------------------------------
// TESTS
// ----------------------------------------------------------------------

func TestInfoEntry_AlwaysHasSeverity(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf)

	log.Info("startup complete")

	entry, err := decode(buf.String())
	require.NoError(t, err)

	assert.Equal(t, "info", entry["severity"])
	assert.Equal(t, "startup complete", entry["message"])

	// timestamp RFC3339 + UTC
	ts, err := time.Parse(time.RFC3339, entry["timestamp"].(string))
	require.NoError(t, err)
	assert.Equal(t, time.UTC, ts.Location())
}

func TestContextField_Preserved(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf)

	log.WithFields(logrus.Fields{
		"user":   "alice",
		"action": "login",
	}).Warn("user activity")

	entry, err := decode(buf.String())
	require.NoError(t, err)

	ctx, ok := entry["context"].(map[string]any)
	require.True(t, ok, "`context` field missing")

	assert.Equal(t, "alice", ctx["user"])
	assert.Equal(t, "login", ctx["action"])
	assert.Equal(t, "warning", entry["severity"])
}

func TestNewlineSuffix(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf)

	log.Error("fatal!")

	raw := buf.String()
	assert.True(t, strings.HasSuffix(raw, "\n"), "log line must end with newline")
}
