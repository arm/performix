// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	t.Run("required args", func(t *testing.T) {
		cfg, err := parseArgs([]string{
			"--output", "syscalls.parquet",
			"--raw-output", "syscall_trace.log",
		})
		if err != nil {
			t.Fatalf("parseArgs failed: %v", err)
		}
		if cfg.outputPath != "syscalls.parquet" || cfg.rawPath != "syscall_trace.log" {
			t.Fatalf("unexpected config: %#v", cfg)
		}
	})

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "missing output",
			args: []string{"--raw-output", "raw.log"},
		},
		{
			name: "missing raw output",
			args: []string{"--output", "syscalls.parquet"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseArgs(tt.args); err == nil {
				t.Fatal("parseArgs succeeded, want error")
			}
		})
	}
}

func TestEnsureParentDir(t *testing.T) {
	nestedPath := filepath.Join(t.TempDir(), "nested", "syscalls.parquet")
	if err := ensureParentDir(nestedPath); err != nil {
		t.Fatalf("ensureParentDir failed: %v", err)
	}
	if info, err := os.Stat(filepath.Dir(nestedPath)); err != nil || !info.IsDir() {
		t.Fatalf("parent directory was not created: info=%v err=%v", info, err)
	}

	if err := ensureParentDir("syscalls.parquet"); err != nil {
		t.Fatalf("ensureParentDir with local file failed: %v", err)
	}
}

func TestParseTraceFile(t *testing.T) {
	dir := t.TempDir()
	rawPath := filepath.Join(dir, "raw.log")
	outputPath := filepath.Join(dir, "syscalls.parquet")

	raw := `1234 1700000000.123456 getpid() = 42 <0.000001>` + "\n"
	if err := os.WriteFile(rawPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("failed to write raw trace: %v", err)
	}

	if err := parseTraceFile(rawPath, outputPath); err != nil {
		t.Fatalf("parseTraceFile failed: %v", err)
	}

	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output trace: %v", err)
	}
	rows := readTraceRows(t, output)
	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1: %#v", len(rows), rows)
	}
	if rows[0].pid == nil || *rows[0].pid != 1234 || rows[0].syscall != "getpid" || rows[0].durationUS == nil || *rows[0].durationUS != 1 || rows[0].result != "42" {
		t.Fatalf("unexpected parsed syscall row: %#v", rows[0])
	}
}

func TestParseTraceFileReadsStraceFFOutputs(t *testing.T) {
	dir := t.TempDir()
	rawPrefix := filepath.Join(dir, "syscall_trace.log")
	outputPath := filepath.Join(dir, "syscalls.parquet")

	raw := strings.Join([]string{
		`1700000000.123456 getpid() = 1234 <0.000001>`,
		`1700000000.223456 read(3,  <unfinished ...>`,
		`1700000000.223466 <... read resumed>"x", 1) = 1 <0.000010>`,
	}, "\n")
	if err := os.WriteFile(rawPrefix+".1234", []byte(raw), 0o644); err != nil {
		t.Fatalf("failed to write raw trace: %v", err)
	}

	if err := parseTraceFile(rawPrefix, outputPath); err != nil {
		t.Fatalf("parseTraceFile failed: %v", err)
	}

	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output trace: %v", err)
	}
	rows := readTraceRows(t, output)
	if len(rows) != 2 {
		t.Fatalf("row count = %d, want 2: %#v", len(rows), rows)
	}
	if rows[0].pid == nil || *rows[0].pid != 1234 || rows[0].syscall != "getpid" {
		t.Fatalf("unexpected first row: %#v", rows[0])
	}
	if rows[1].pid == nil || *rows[1].pid != 1234 || rows[1].syscall != "read" || rows[1].args != `3,  "x", 1` {
		t.Fatalf("unexpected resumed row: %#v", rows[1])
	}
}

func TestParseTraceFileSortsStraceFFEventsByTimestamp(t *testing.T) {
	dir := t.TempDir()
	rawPrefix := filepath.Join(dir, "syscall_trace.log")
	outputPath := filepath.Join(dir, "syscalls.parquet")

	files := map[string]string{
		rawPrefix + ".2222": `1700000000.300000 close(3) = 0 <0.000003>` + "\n",
		rawPrefix + ".1111": `1700000000.100000 getpid() = 1111 <0.000001>` + "\n",
		rawPrefix + ".3333": `1700000000.200000 write(1, "x", 1) = 1 <0.000002>` + "\n",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("failed to write raw trace %s: %v", path, err)
		}
	}

	if err := parseTraceFile(rawPrefix, outputPath); err != nil {
		t.Fatalf("parseTraceFile failed: %v", err)
	}

	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output trace: %v", err)
	}
	rows := readTraceRows(t, output)
	got := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.pid == nil {
			t.Fatalf("row missing pid: %#v", row)
		}
		got = append(got, row.syscall)
	}
	want := []string{"getpid", "write", "close"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("syscall order = %#v, want %#v; rows=%#v", got, want, rows)
	}
}

func TestDiscoverTraceInputs(t *testing.T) {
	dir := t.TempDir()
	rawPrefix := filepath.Join(dir, "syscall_trace.log")

	for _, path := range []string{
		rawPrefix,
		rawPrefix + ".1002",
		rawPrefix + ".metadata",
	} {
		if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
			t.Fatalf("failed to write %s: %v", path, err)
		}
	}
	if err := os.Mkdir(rawPrefix+".1003", 0o755); err != nil {
		t.Fatalf("failed to create directory trace candidate: %v", err)
	}

	inputs, err := discoverTraceInputs(rawPrefix)
	if err != nil {
		t.Fatalf("discoverTraceInputs failed: %v", err)
	}

	got := make([]string, 0, len(inputs))
	gotDefaultPIDs := make([]int64, 0, len(inputs))
	for _, input := range inputs {
		got = append(got, filepath.Base(input.path))
		if input.hasDefaultPID {
			gotDefaultPIDs = append(gotDefaultPIDs, input.defaultPID)
		}
	}

	want := []string{
		"syscall_trace.log",
		"syscall_trace.log.1002",
		"syscall_trace.log.metadata",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inputs = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(gotDefaultPIDs, []int64{1002}) {
		t.Fatalf("default pids = %#v, want [1002]", gotDefaultPIDs)
	}
}

func TestPIDFromTracePath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want int64
		ok   bool
	}{
		{
			name: "numeric suffix",
			path: "/tmp/syscall_trace.log.1234",
			want: 1234,
			ok:   true,
		},
		{
			name: "non numeric suffix",
			path: "/tmp/syscall_trace.log.stderr",
		},
		{
			name: "different prefix",
			path: "/tmp/other.log.1234",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := pidFromTracePath(tt.path, "/tmp/syscall_trace.log")
			if got != tt.want || ok != tt.ok {
				t.Fatalf("pidFromTracePath(%q) = %d, %v; want %d, %v", tt.path, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestParseTraceFileErrors(t *testing.T) {
	dir := t.TempDir()

	if err := parseTraceFile(filepath.Join(dir, "missing.log"), filepath.Join(dir, "out.parquet")); err == nil {
		t.Fatal("parseTraceFile succeeded for missing raw trace, want error")
	}

	rawPath := filepath.Join(dir, "raw.log")
	if err := os.WriteFile(rawPath, []byte(""), 0o644); err != nil {
		t.Fatalf("failed to write raw trace: %v", err)
	}
	if err := parseTraceFile(rawPath, dir); err == nil {
		t.Fatal("parseTraceFile succeeded with output path set to directory, want error")
	}
}

func TestParseTraceReturnsScannerErrors(t *testing.T) {
	err := parseTrace(errorReader{}, &strings.Builder{})
	if err == nil || err.Error() != "read failed" {
		t.Fatalf("parseTrace error = %v, want read failed", err)
	}
}

func TestParseTraceReturnsWriterErrors(t *testing.T) {
	err := parseTrace(strings.NewReader(""), errorWriter{})
	if err == nil || !strings.Contains(err.Error(), "write") {
		t.Fatalf("parseTrace error = %v, want write error", err)
	}
}

func TestCollectTraceEventsReportsIncompleteResumedPairs(t *testing.T) {
	input := strings.Join([]string{
		`1700000000.100000 read(3,  <unfinished ...>`,
		`1700000000.100010 <... futex resumed>) = 0 <0.000001>`,
		`1700000000.200000 write(1,  <unfinished ...>`,
		`1700000000.300000 close(3) = 0 <0.000003>`,
	}, "\n")

	var stats parseTraceStats
	events, err := collectTraceEvents(strings.NewReader(input), &stats, "trace.4321", 4321, true)
	if err != nil {
		t.Fatalf("collectTraceEvents failed: %v", err)
	}

	if len(events) != 1 || events[0].syscall != "close" {
		t.Fatalf("events = %#v, want only completed close syscall", events)
	}
	if stats.unmatchedResumed != 1 || stats.droppedUnfinished != 1 || stats.failed != 2 {
		t.Fatalf("stats = %#v, want one unmatched resumed and one incomplete unfinished", stats)
	}
	if len(stats.examples) != 2 ||
		!strings.Contains(stats.examples[0], `resumed syscall "futex" does not match unfinished syscall "read"`) ||
		!strings.Contains(stats.examples[1], "unfinished syscall did not resume") {
		t.Fatalf("examples = %#v, want mismatch and incomplete unfinished diagnostics", stats.examples)
	}
}

func TestParseTraceStatsReportsUnfinishedAndMalformedLines(t *testing.T) {
	stats := parseTraceStats{
		parsed:            3,
		failed:            1,
		resumed:           2,
		unmatchedResumed:  1,
		droppedUnfinished: 1,
		examples:          []string{"trace.log:5: line did not match syscall format: unexpected"},
	}

	var output bytes.Buffer
	stats.report(&output)

	got := output.String()
	for _, want := range []string{
		"reconstructed 2 resumed syscall line(s)",
		"skipped 1 unmatched resumed and 1 incomplete unfinished syscall line(s)",
		"reported 1 parse issue(s)",
		"trace.log:5: line did not match syscall format: unexpected",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("report output %q missing %q", got, want)
		}
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}
