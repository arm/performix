// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package logging

import (
	"encoding/json"
	"time"

	"github.com/sirupsen/logrus"
)

// JSONLinesFormatter turns a logrus.Entry into a single-line JSON object:
//
//	{
//	  "timestamp": "<RFC3339 UTC>",
//	  "severity":  "<level>",      // always present
//	  "message":   "<text>",
//	  "context":   { ... }         // optional, original Entry.Data
//	}
//
// Each call appends '\n', producing valid JSON-Lines output.
type JSONLinesFormatter struct{}

// Format implements logrus.Formatter.
func (f *JSONLinesFormatter) Format(e *logrus.Entry) ([]byte, error) {
	record := map[string]any{
		"timestamp": e.Time.UTC().Format(time.RFC3339),
		"severity":  e.Level.String(),
		"message":   e.Message,
	}
	if len(e.Data) > 0 {
		record["context"] = e.Data
	}

	b, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
