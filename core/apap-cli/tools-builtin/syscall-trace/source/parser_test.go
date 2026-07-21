// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
)

func TestParseTrace(t *testing.T) {
	input := strings.Join([]string{
		`1233 1700000000.123456 read(3, "hello", 5) = 5 <0.000012>`,
		`[pid 1234] 1700000000.223456 openat(AT_FDCWD, "/missing", O_RDONLY) = -1 ENOENT (No such file or directory) <0.000034>`,
		`5678 1700000000.323456 close(3) = 0`,
		`[pid 1234] 1700000000.423456 read(3,  <unfinished ...>`,
		`[pid 1234] <... read resumed>"x", 1) = 1 <0.000010>`,
	}, "\n")

	var output bytes.Buffer
	if err := parseTrace(strings.NewReader(input), &output); err != nil {
		t.Fatalf("parseTrace failed: %v", err)
	}

	rows := readTraceRows(t, output.Bytes())
	want := []traceRow{
		{
			timestampUS: 1700000000123456,
			pid:         int64Ptr(1233),
			syscall:     "read",
			args:        `3, "hello", 5`,
			durationUS:  int64Ptr(12),
			result:      "5",
		},
		{
			timestampUS: 1700000000223456,
			pid:         int64Ptr(1234),
			syscall:     "openat",
			args:        `AT_FDCWD, "/missing", O_RDONLY`,
			durationUS:  int64Ptr(34),
			result:      "-1 ENOENT (No such file or directory)",
			errno:       stringPtr("ENOENT"),
		},
		{
			timestampUS: 1700000000323456,
			pid:         int64Ptr(5678),
			syscall:     "close",
			args:        "3",
			result:      "0",
		},
		{
			timestampUS: 1700000000423456,
			pid:         int64Ptr(1234),
			syscall:     "read",
			args:        `3,  "x", 1`,
			durationUS:  int64Ptr(10),
			result:      "1",
		},
	}

	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows = %#v, want %#v", rows, want)
	}
}

func TestParseTraceReconstructsResumedSyscallsWithDefaultPID(t *testing.T) {
	input := strings.Join([]string{
		`1700000000.100000 read(3,  <unfinished ...>`,
		`1700000000.100044 <... read resumed>"abc", 3) = 3 <0.000044>`,
		`1700000000.200000 futex(0xffff, FUTEX_WAKE_PRIVATE, 1 <unfinished ...>`,
		`1700000000.200029 <... futex resumed>) = 1 <0.000029>`,
	}, "\n")

	var output bytes.Buffer
	if err := parseTraceReader(strings.NewReader(input), &output, "trace.4321", 4321, true); err != nil {
		t.Fatalf("parseTraceReader failed: %v", err)
	}

	rows := readTraceRows(t, output.Bytes())
	want := []traceRow{
		{
			timestampUS: 1700000000100000,
			pid:         int64Ptr(4321),
			syscall:     "read",
			args:        `3,  "abc", 3`,
			durationUS:  int64Ptr(44),
			result:      "3",
		},
		{
			timestampUS: 1700000000200000,
			pid:         int64Ptr(4321),
			syscall:     "futex",
			args:        `0xffff, FUTEX_WAKE_PRIVATE, 1`,
			durationUS:  int64Ptr(29),
			result:      "1",
		},
	}

	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows = %#v, want %#v", rows, want)
	}
}

func TestParseLineRejectsMalformedTimestamps(t *testing.T) {
	_, status, err := parseLine(`1234 not-a-time read(3, "hello", 5) = 5 <0.000012>`)
	if status != parseLineFailed || err == nil {
		t.Fatal("parseLine accepted malformed timestamp")
	}
}

func TestParseLineHandlesRepresentativeStraceOutput(t *testing.T) {
	tests := []struct {
		name string
		line string
		want traceEvent
	}{
		{
			name: "string argument contains punctuation",
			line: `4320 1700000001.000001 write(1, "a=b) c <d>\n", 11) = 11 <0.000002>`,
			want: traceEvent{
				timestampUS: 1700000001000001,
				pid:         4320,
				syscall:     "write",
				args:        `1, "a=b) c <d>\n", 11`,
				durationUS:  2,
				hasDuration: true,
				result:      "11",
			},
		},
		{
			name: "pid prefix from strace -f",
			line: `[pid 4321] 1700000001.000002 futex(0xffff, FUTEX_WAIT_PRIVATE, 2, NULL) = -1 EAGAIN (Resource temporarily unavailable) <0.000003>`,
			want: traceEvent{
				timestampUS: 1700000001000002,
				pid:         4321,
				syscall:     "futex",
				args:        "0xffff, FUTEX_WAIT_PRIVATE, 2, NULL",
				durationUS:  3,
				hasDuration: true,
				result:      "-1 EAGAIN (Resource temporarily unavailable)",
				errno:       "EAGAIN",
			},
		},
		{
			name: "numeric pid prefix",
			line: `4322 1700000001.000003 clock_gettime(CLOCK_MONOTONIC, {tv_sec=1, tv_nsec=2}) = 0 <0.000004>`,
			want: traceEvent{
				timestampUS: 1700000001000003,
				pid:         4322,
				syscall:     "clock_gettime",
				args:        "CLOCK_MONOTONIC, {tv_sec=1, tv_nsec=2}",
				durationUS:  4,
				hasDuration: true,
				result:      "0",
			},
		},
		{
			name: "pointer result with decoded path",
			line: `4323 1700000001.000004 mmap(NULL, 8192, PROT_READ|PROT_WRITE, MAP_PRIVATE|MAP_ANONYMOUS, -1, 0) = 0xffff9c000000 <0.000005>`,
			want: traceEvent{
				timestampUS: 1700000001000004,
				pid:         4323,
				syscall:     "mmap",
				args:        "NULL, 8192, PROT_READ|PROT_WRITE, MAP_PRIVATE|MAP_ANONYMOUS, -1, 0",
				durationUS:  5,
				hasDuration: true,
				result:      "0xffff9c000000",
			},
		},
		{
			name: "exit group without duration",
			line: `4324 1700000001.000005 exit_group(0) = ?`,
			want: traceEvent{
				timestampUS: 1700000001000005,
				pid:         4324,
				syscall:     "exit_group",
				args:        "0",
				result:      "?",
			},
		},
		{
			name: "killed by signal result",
			line: `4325 1700000001.000006 recvfrom(3, 0xffff, 4096, 0, NULL, NULL) = ? ERESTARTSYS (To be restarted if SA_RESTART is set) <0.000006>`,
			want: traceEvent{
				timestampUS: 1700000001000006,
				pid:         4325,
				syscall:     "recvfrom",
				args:        "3, 0xffff, 4096, 0, NULL, NULL",
				durationUS:  6,
				hasDuration: true,
				result:      "? ERESTARTSYS (To be restarted if SA_RESTART is set)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, status, err := parseLine(tt.line)
			if status != parseLineParsed {
				t.Fatalf("parseLine(%q) status = %v err = %v, want parsed", tt.line, status, err)
			}
			if got != tt.want {
				t.Fatalf("parseLine(%q) = %#v, want %#v", tt.line, got, tt.want)
			}
		})
	}
}

type traceRow struct {
	timestampUS int64
	pid         *int64
	syscall     string
	args        string
	durationUS  *int64
	result      string
	errno       *string
}

func readTraceRows(t *testing.T, data []byte) []traceRow {
	t.Helper()

	parquetReader, err := file.NewParquetReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("failed to open Parquet output: %v", err)
	}
	defer parquetReader.Close()

	arrowReader, err := pqarrow.NewFileReader(
		parquetReader,
		pqarrow.ArrowReadProperties{BatchSize: arrowRecordBatchRows},
		memory.DefaultAllocator,
	)
	if err != nil {
		t.Fatalf("failed to create Arrow reader: %v", err)
	}

	recordReader, err := arrowReader.GetRecordReader(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create record reader: %v", err)
	}
	defer recordReader.Release()

	rows := []traceRow{}
	for recordReader.Next() {
		record := recordReader.RecordBatch()
		tsCol := record.Column(0).(*array.Timestamp)
		pidCol := record.Column(1).(*array.Int64)
		syscallCol := record.Column(2).(*array.String)
		argsCol := record.Column(3).(*array.String)
		durationCol := record.Column(4).(*array.Int64)
		resultCol := record.Column(5).(*array.String)
		errnoCol := record.Column(6).(*array.String)

		for i := 0; i < int(record.NumRows()); i++ {
			rows = append(rows, traceRow{
				timestampUS: int64(tsCol.Value(i)),
				pid:         nullableInt64(pidCol, i),
				syscall:     syscallCol.Value(i),
				args:        argsCol.Value(i),
				durationUS:  nullableInt64(durationCol, i),
				result:      resultCol.Value(i),
				errno:       nullableString(errnoCol, i),
			})
		}
	}
	if err := recordReader.Err(); err != nil {
		t.Fatalf("failed to read Parquet rows: %v", err)
	}

	return rows
}

func nullableInt64(col *array.Int64, index int) *int64 {
	if col.IsNull(index) {
		return nil
	}
	value := col.Value(index)
	return &value
}

func nullableString(col *array.String, index int) *string {
	if col.IsNull(index) {
		return nil
	}
	value := col.Value(index)
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}

func stringPtr(value string) *string {
	return &value
}

func TestParseLineFailsUnexpectedLines(t *testing.T) {
	tests := []string{
		`this is not a syscall line`,
		`--- SIGCHLD {si_signo=SIGCHLD, si_code=CLD_EXITED, si_pid=1234} ---`,
		`+++ exited with 0 +++`,
		`strace: Process 1234 attached`,
		``,
	}

	for _, tt := range tests {
		t.Run(tt, func(t *testing.T) {
			if got, status, err := parseLine(tt); status != parseLineFailed || err == nil {
				t.Fatalf("parseLine(%q) = %#v status=%v err=%v, want failed with error", tt, got, status, err)
			}
		})
	}
}

func TestEpochToUTCPreservesMicroseconds(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "six digit microseconds",
			raw:  "1700000000.123456",
			want: "2023-11-14T22:13:20.123456Z",
		},
		{
			name: "right pads shorter fractions",
			raw:  "1700000000.1",
			want: "2023-11-14T22:13:20.100000Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := epochToUTC(tt.raw)
			if !ok {
				t.Fatalf("epochToUTC(%q) failed", tt.raw)
			}
			if got != tt.want {
				t.Fatalf("epochToUTC(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestEpochToUTCRejectsInvalidTimestamp(t *testing.T) {
	tests := []string{
		"not-a-number",
		"1700000000",
		"1700000000.",
		"1700000000.invalid",
		"1700000000.1234567",
	}

	for _, tt := range tests {
		t.Run(tt, func(t *testing.T) {
			if _, ok := epochToUTC(tt); ok {
				t.Fatalf("epochToUTC accepted invalid timestamp %q", tt)
			}
		})
	}
}
