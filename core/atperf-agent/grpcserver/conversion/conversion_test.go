// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conversion

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

func TestProtoFromLogEntry_And_Back_RoundTrip(t *testing.T) {
	// Use UTC to avoid location surprises in RFC3339Nano formatting
	now := time.Now().UTC()

	original := &logrus.Entry{
		Level:   logrus.WarnLevel,
		Message: "something happened",
		Time:    now,
		Data: map[string]interface{}{
			"key":    "val",
			"num":    123.0, // structpb treats numbers as float64
			"nested": map[string]interface{}{"ok": true},
		},
	}

	// Convert to proto
	p := ProtoFromLogEntry(original)
	require.NotNil(t, p, "proto should not be nil")

	// Basic proto assertions
	assert.Equal(t, "warning", p.Level, "logrus.WarnLevel should stringify to 'warning'")
	assert.Equal(t, original.Message, p.Message)
	assert.Equal(t, original.Time.Format(time.RFC3339Nano), p.Timestamp)
	require.NotNil(t, p.Metadata, "metadata should not be nil")

	// Convert back to logrus.Entry
	var back logrus.Entry
	level, msg := LogEntryFromProto(p, &back)

	// Returned values
	assert.Equal(t, logrus.WarnLevel, level)
	assert.Equal(t, original.Message, msg)

	// Entry fields
	assert.Equal(t, logrus.WarnLevel, back.Level)
	assert.Equal(t, original.Message, back.Message)
	assert.True(t, back.Time.Equal(original.Time), "parsed time should equal original time")

	// Metadata round-trip (AsMap returns JSON-compatible types)
	wantMeta := p.Metadata.AsMap()
	assert.EqualValues(t, wantMeta, back.Data, "metadata should round-trip via structpb")
}

func TestLogEntryFromProto_And_Back_UnknownLevel_EmptyTimestamp_NilMetadata(t *testing.T) {
	// Build a proto with unknown level, empty timestamp, and nil metadata
	p := &targetagentproto.LogEntry{
		Level:     "weird-level",
		Message:   "msg",
		Timestamp: "",
		Metadata:  nil,
	}

	// Convert to logrus.Entry
	var out logrus.Entry
	level, msg := LogEntryFromProto(p, &out)

	// Defaults and fallbacks
	assert.Equal(t, logrus.InfoLevel, level, "unknown level should default to Info")
	assert.Equal(t, "msg", msg)
	assert.Equal(t, logrus.InfoLevel, out.Level)
	assert.Equal(t, "msg", out.Message)
	assert.True(t, out.Time.IsZero(), "empty/invalid timestamp should leave time zero")
	require.NotNil(t, out.Data, "out.Data should be initialized")
	assert.Empty(t, out.Data, "nil metadata should become an empty map")

	// Convert back to proto from the partially-filled entry
	p2 := ProtoFromLogEntry(&out)
	require.NotNil(t, p2)

	// Since out.Time is zero, RFC3339Nano gives year 1; level defaults to "info"
	assert.Equal(t, "info", p2.Level)
	assert.Equal(t, "msg", p2.Message)
	// Zero time in RFC3339Nano is "0001-01-01T00:00:00Z" (exact string)
	assert.Equal(t, out.Time.Format(time.RFC3339Nano), p2.Timestamp)
	require.NotNil(t, p2.Metadata)
	assert.Empty(t, p2.Metadata.AsMap())
}
