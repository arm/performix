// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Package main normalizes raw strace output into the Performix syscall event
// Parquet schema. The tool integration is responsible for invoking strace.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	exitUsage = 2
	exitParse = 4

	maxParseFailureExamples = 10
)

type config struct {
	outputPath string
	rawPath    string
}

func main() {
	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitUsage)
	}

	if err := ensureParentDir(cfg.outputPath); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create output directory: %v\n", err)
		os.Exit(exitUsage)
	}
	if err := ensureParentDir(cfg.rawPath); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create raw output directory: %v\n", err)
		os.Exit(exitUsage)
	}

	if err := parseTraceFile(cfg.rawPath, cfg.outputPath); err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse strace output: %v\n", err)
		os.Exit(exitParse)
	}
}

func parseArgs(args []string) (config, error) {
	var cfg config

	fs := flag.NewFlagSet("syscall-trace", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.outputPath, "output", "", "Path to output Parquet.")
	fs.StringVar(&cfg.rawPath, "raw-output", "", "Path to raw strace output.")

	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if cfg.outputPath == "" {
		return cfg, errors.New("--output is required")
	}
	if cfg.rawPath == "" {
		return cfg, errors.New("--raw-output is required")
	}

	return cfg, nil
}

func ensureParentDir(path string) error {
	parent := filepath.Dir(path)
	if parent == "." || parent == "" {
		return nil
	}
	return os.MkdirAll(parent, 0o755)
}

func parseTraceFile(rawPath, outputPath string) error {
	inputs, err := discoverTraceInputs(rawPath)
	if err != nil {
		return err
	}

	output, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer output.Close()

	return parseTraceFiles(inputs, output)
}

func parseTrace(input io.Reader, output io.Writer) error {
	return parseTraceReader(input, output, "<input>", 0, false)
}

type traceInputFile struct {
	path          string
	defaultPID    int64
	hasDefaultPID bool
}

func discoverTraceInputs(rawPath string) ([]traceInputFile, error) {
	var inputs []traceInputFile

	if info, err := os.Stat(rawPath); err == nil && !info.IsDir() {
		inputs = append(inputs, traceInputFile{path: rawPath})
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	matches, err := filepath.Glob(rawPath + ".*")
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			continue
		}
		input := traceInputFile{path: path}
		if pid, ok := pidFromTracePath(path, rawPath); ok {
			input.defaultPID = pid
			input.hasDefaultPID = true
		}
		inputs = append(inputs, input)
	}

	if len(inputs) == 0 {
		return nil, fmt.Errorf("no strace output files found for %s", rawPath)
	}
	return inputs, nil
}

func pidFromTracePath(path, rawPath string) (int64, bool) {
	suffix := strings.TrimPrefix(path, rawPath+".")
	if suffix == path || suffix == "" {
		return 0, false
	}
	pid, err := strconv.ParseInt(suffix, 10, 64)
	if err != nil {
		return 0, false
	}
	return pid, true
}

func parseTraceFiles(inputs []traceInputFile, output io.Writer) error {
	var stats parseTraceStats
	var events []traceEvent
	for _, inputMeta := range inputs {
		input, err := os.Open(inputMeta.path)
		if err != nil {
			return err
		}
		fileEvents, err := collectTraceEvents(input, &stats, inputMeta.path, inputMeta.defaultPID, inputMeta.hasDefaultPID)
		closeErr := input.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		events = append(events, fileEvents...)
	}

	stats.report(os.Stderr)
	return writeTraceEvents(events, output)
}

func parseTraceReader(input io.Reader, output io.Writer, source string, defaultPID int64, hasDefaultPID bool) error {
	var stats parseTraceStats
	events, err := collectTraceEvents(input, &stats, source, defaultPID, hasDefaultPID)
	if err != nil {
		return err
	}
	stats.report(os.Stderr)
	return writeTraceEvents(events, output)
}

func writeTraceEvents(events []traceEvent, output io.Writer) error {
	writer, err := newParquetTraceWriter(output)
	if err != nil {
		return err
	}

	sort.SliceStable(events, func(i, j int) bool {
		if events[i].timestampUS != events[j].timestampUS {
			return events[i].timestampUS < events[j].timestampUS
		}
		if events[i].pid != events[j].pid {
			return events[i].pid < events[j].pid
		}
		return events[i].syscall < events[j].syscall
	})

	for _, event := range events {
		if err := writer.Append(event); err != nil {
			_ = writer.Close()
			return err
		}
	}
	return writer.Close()
}

func collectTraceEvents(input io.Reader, stats *parseTraceStats, source string, defaultPID int64, hasDefaultPID bool) ([]traceEvent, error) {
	var events []traceEvent
	scanner := newLineScanner(input)
	lineNo := 0
	// strace -ff reduces interleaving, but interrupted syscalls can still be
	// split into unfinished/resumed pairs in per-PID logs and manual captures.
	unfinishedByPID := map[int64]unfinishedTraceEvent{}
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		parsed := parseTraceLine(line, defaultPID, hasDefaultPID)
		switch parsed.status {
		case parseLineParsed:
			stats.parsed++
			events = append(events, parsed.event)
		case parseLineUnfinished:
			stats.unfinished++
			parsed.unfinished.lineNo = lineNo
			if existing, ok := unfinishedByPID[parsed.unfinished.pid]; ok {
				stats.droppedUnfinished++
				stats.recordFailure(source, existing.lineNo, existing.line, fmt.Errorf("unfinished syscall was replaced before it resumed"))
			}
			unfinishedByPID[parsed.unfinished.pid] = parsed.unfinished
			continue
		case parseLineResumed:
			unfinished, ok := unfinishedByPID[parsed.resumed.pid]
			if !ok {
				stats.unmatchedResumed++
				stats.recordFailure(source, lineNo, line, fmt.Errorf("resumed syscall without matching unfinished syscall"))
				continue
			}
			event, err := finishResumedSyscall(unfinished, parsed.resumed)
			if err != nil {
				stats.unmatchedResumed++
				stats.recordFailure(source, lineNo, line, err)
				delete(unfinishedByPID, parsed.resumed.pid)
				continue
			}
			delete(unfinishedByPID, parsed.resumed.pid)
			stats.resumed++
			stats.parsed++
			events = append(events, event)
		case parseLineFailed:
			stats.recordFailure(source, lineNo, line, parsed.err)
			continue
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	for _, unfinished := range unfinishedByPID {
		stats.droppedUnfinished++
		stats.recordFailure(source, unfinished.lineNo, unfinished.line, fmt.Errorf("unfinished syscall did not resume"))
	}
	return events, nil
}

type parseTraceStats struct {
	parsed            int
	failed            int
	unfinished        int
	resumed           int
	unmatchedResumed  int
	droppedUnfinished int
	examples          []string
}

func (s *parseTraceStats) recordFailure(source string, lineNo int, line string, err error) {
	s.failed++
	if len(s.examples) >= maxParseFailureExamples {
		return
	}
	s.examples = append(
		s.examples,
		fmt.Sprintf("%s:%d: %v: %s", source, lineNo, err, line),
	)
}

func (s parseTraceStats) report(output io.Writer) {
	if s.failed == 0 && s.unmatchedResumed == 0 && s.droppedUnfinished == 0 && s.resumed == 0 {
		return
	}
	if s.resumed > 0 {
		fmt.Fprintf(
			output,
			"syscall-trace parser reconstructed %d resumed syscall line(s); parsed=%d\n",
			s.resumed,
			s.parsed,
		)
	}
	if s.unmatchedResumed > 0 || s.droppedUnfinished > 0 {
		fmt.Fprintf(
			output,
			"syscall-trace parser skipped %d unmatched resumed and %d incomplete unfinished syscall line(s); parsed=%d\n",
			s.unmatchedResumed,
			s.droppedUnfinished,
			s.parsed,
		)
	}
	if s.failed == 0 {
		return
	}
	fmt.Fprintf(
		output,
		"syscall-trace parser reported %d parse issue(s); parsed=%d\n",
		s.failed,
		s.parsed,
	)
	for _, example := range s.examples {
		fmt.Fprintln(output, example)
	}
}
