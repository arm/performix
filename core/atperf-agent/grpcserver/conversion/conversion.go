// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conversion

import (
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// ProtoFromLogEntry converts a logrus.Entry to a targetagentproto.LogEntry
func ProtoFromLogEntry(entry *logrus.Entry) *targetagentproto.LogEntry {
	meta, _ := structpb.NewStruct(entry.Data)
	return &targetagentproto.LogEntry{
		Level:     entry.Level.String(),
		Message:   entry.Message,
		Timestamp: entry.Time.Format(time.RFC3339Nano),
		Metadata:  meta,
	}
}

// LogEntryFromProto converts a targetagentproto.LogEntry to a logrus.Entry
func LogEntryFromProto(entry *targetagentproto.LogEntry, out *logrus.Entry) (logrus.Level, string) {
	level := logrus.InfoLevel
	if parsedLevel, err := logrus.ParseLevel(entry.Level); err == nil {
		level = parsedLevel
	}
	out.Level = level
	out.Message = entry.Message
	if entry.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err == nil {
			out.Time = t
		}
	}
	if out.Data == nil {
		out.Data = make(map[string]interface{})
	}
	if entry.Metadata != nil {
		for k, v := range entry.Metadata.AsMap() {
			if _, exists := out.Data[k]; !exists {
				out.Data[k] = v
			}
		}
	}

	return level, entry.Message
}
