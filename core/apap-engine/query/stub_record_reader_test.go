// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package query

import (
	"github.com/apache/arrow-go/v18/arrow"
)

// stubRecordReader is a minimal RecordReader for tests that allows us to inject nil or empty records.
type stubRecordReader struct {
	schema  *arrow.Schema
	records []arrow.RecordBatch
	idx     int
}

func (s *stubRecordReader) Retain()  {}
func (s *stubRecordReader) Release() {}

func (s *stubRecordReader) Schema() *arrow.Schema { return s.schema }

func (s *stubRecordReader) Next() bool {
	if s.idx >= len(s.records) {
		return false
	}
	s.idx++
	return true
}

func (s *stubRecordReader) Record() arrow.RecordBatch {
	if s.idx == 0 || s.idx > len(s.records) {
		return nil
	}
	return s.records[s.idx-1]
}

func (s *stubRecordReader) Err() error { return nil }

func (r *stubRecordReader) RecordBatch() arrow.RecordBatch { return nil }
