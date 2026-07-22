// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package message

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildErrorPayloadMessage(t *testing.T) {
	msg := New(EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": "run-1"})

	payload := BuildErrorPayload(msg, nil)
	require.NotNil(t, payload)

	catalogMsg, err := LookupMessage(msg)
	require.NoError(t, err)

	assert.Equal(t, msg.Code(), payload.Code)
	assert.Equal(t, msg.Locale(), payload.Locale)
	assert.Equal(t, msg.Metadata(), payload.Metadata)
	assert.Equal(t, catalogMsg.Severity, payload.Severity)
	assert.Equal(t, catalogMsg.Message, payload.Message)
	assert.Equal(t, catalogMsg.Explanation, payload.Explanation)
	assert.Equal(t, catalogMsg.Advice, payload.Advice)
}

func TestBuildErrorPayloadChildren(t *testing.T) {
	msg := New(EngineRunDoesNotExist).WithCause(errors.New("inner error"))

	payload := BuildErrorPayload(msg, nil)
	require.NotNil(t, payload)
	require.Len(t, payload.Children, 1)
	assert.Equal(t, "inner error", payload.Children[0].Message)
	assert.Equal(t, string(SeverityError), payload.Children[0].Severity)
}

func TestBuildErrorPayloadNonMessageFormatting(t *testing.T) {
	payload := BuildErrorPayload(errors.New("boom"), &ErrorPayloadOptions{
		FormatNonMessage: func(err error) string {
			return "formatted: " + err.Error()
		},
	})

	require.NotNil(t, payload)
	assert.Equal(t, "formatted: boom", payload.Message)
	assert.Equal(t, string(SeverityError), payload.Severity)
}

func TestBuildErrorPayloadLookupOverride(t *testing.T) {
	msg := New(EngineRunDoesNotExist)
	called := false
	payload := BuildErrorPayload(msg, &ErrorPayloadOptions{
		LookupMessage: func(err error) (*CatalogMessage, error) {
			called = true
			return &CatalogMessage{
				Code:        "override.code",
				Severity:    SeverityWarning,
				Message:     "override message",
				Explanation: "override explanation",
				Advice:      "override advice",
			}, nil
		},
	})

	require.NotNil(t, payload)
	assert.True(t, called)
	assert.Equal(t, SeverityWarning, payload.Severity)
	assert.Equal(t, "override message", payload.Message)
	assert.Equal(t, "override explanation", payload.Explanation)
	assert.Equal(t, "override advice", payload.Advice)
}
