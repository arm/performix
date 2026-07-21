// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	lineRE       = regexp.MustCompile(`^(?:\[pid\s+([0-9]+)\]\s+)?(?:(\d+)\s+)?(\d+\.\d+)\s+([A-Za-z0-9_]+)\((.*)\)\s+=\s+(.+?)(?:\s+<([0-9.]+)>)?$`)
	unfinishedRE = regexp.MustCompile(`^(?:\[pid\s+([0-9]+)\]\s+)?(?:(\d+)\s+)?(\d+\.\d+)\s+([A-Za-z0-9_]+)\((.*)<unfinished \.\.\.>$`)
	resumedRE    = regexp.MustCompile(`^(?:\[pid\s+([0-9]+)\]\s+)?(?:(\d+)\s+)?(?:(\d+\.\d+)\s+)?<\.\.\.\s+([A-Za-z0-9_]+)\s+resumed>(.*)\)\s+=\s+(.+?)(?:\s+<([0-9.]+)>)?$`)
	errnoRE      = regexp.MustCompile(`^-\d+\s+([A-Z][A-Z0-9_]+)(?:\s|$)`)
)

type traceEvent struct {
	timestampUS int64
	pid         int64
	syscall     string
	args        string
	durationUS  int64
	hasDuration bool
	result      string
	errno       string
}

type parseLineStatus int

const (
	parseLineParsed parseLineStatus = iota
	parseLineFailed
	parseLineUnfinished
	parseLineResumed
)

type unfinishedTraceEvent struct {
	timestampUS int64
	pid         int64
	syscall     string
	argsPrefix  string
	lineNo      int
	line        string
}

type resumedTraceEvent struct {
	pid         int64
	syscall     string
	argsSuffix  string
	durationUS  int64
	hasDuration bool
	result      string
	errno       string
}

type parsedTraceLine struct {
	event      traceEvent
	unfinished unfinishedTraceEvent
	resumed    resumedTraceEvent
	status     parseLineStatus
	err        error
}

func newLineScanner(input io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	return scanner
}

func parseLine(line string) (traceEvent, parseLineStatus, error) {
	parsed := parseTraceLine(line, 0, false)
	return parsed.event, parsed.status, parsed.err
}

func parseTraceLine(line string, defaultPID int64, hasDefaultPID bool) parsedTraceLine {
	line = strings.TrimSpace(line)

	if match := unfinishedRE.FindStringSubmatch(line); match != nil {
		event, err := parseUnfinishedMatch(match, defaultPID, hasDefaultPID)
		if err != nil {
			return parsedTraceLine{status: parseLineFailed, err: err}
		}
		event.line = line
		return parsedTraceLine{unfinished: event, status: parseLineUnfinished}
	}

	if match := resumedRE.FindStringSubmatch(line); match != nil {
		event, err := parseResumedMatch(match, defaultPID, hasDefaultPID)
		if err != nil {
			return parsedTraceLine{status: parseLineFailed, err: err}
		}
		return parsedTraceLine{resumed: event, status: parseLineResumed}
	}

	match := lineRE.FindStringSubmatch(line)
	if match == nil {
		return parsedTraceLine{status: parseLineFailed, err: fmt.Errorf("line did not match syscall format")}
	}

	event, err := parseCompletedMatch(match, defaultPID, hasDefaultPID)
	if err != nil {
		return parsedTraceLine{status: parseLineFailed, err: err}
	}
	return parsedTraceLine{event: event, status: parseLineParsed}
}

func parseCompletedMatch(match []string, defaultPID int64, hasDefaultPID bool) (traceEvent, error) {
	timestampUS, err := epochToMicroseconds(match[3])
	if err != nil {
		return traceEvent{}, fmt.Errorf("invalid timestamp %q: %w", match[3], err)
	}

	pid, err := parsePID(match[1], match[2], defaultPID, hasDefaultPID)
	if err != nil {
		return traceEvent{}, err
	}

	durationUS, hasDuration, err := parseOptionalDuration(match[7])
	if err != nil {
		return traceEvent{}, err
	}

	result := strings.TrimSpace(match[6])
	return traceEvent{
		timestampUS: timestampUS,
		pid:         pid,
		syscall:     match[4],
		args:        match[5],
		durationUS:  durationUS,
		hasDuration: hasDuration,
		result:      result,
		errno:       parseErrno(result),
	}, nil
}

func parseUnfinishedMatch(match []string, defaultPID int64, hasDefaultPID bool) (unfinishedTraceEvent, error) {
	timestampUS, err := epochToMicroseconds(match[3])
	if err != nil {
		return unfinishedTraceEvent{}, fmt.Errorf("invalid timestamp %q: %w", match[3], err)
	}

	pid, err := parsePID(match[1], match[2], defaultPID, hasDefaultPID)
	if err != nil {
		return unfinishedTraceEvent{}, err
	}

	return unfinishedTraceEvent{
		timestampUS: timestampUS,
		pid:         pid,
		syscall:     match[4],
		argsPrefix:  match[5],
	}, nil
}

func parseResumedMatch(match []string, defaultPID int64, hasDefaultPID bool) (resumedTraceEvent, error) {
	pid, err := parsePID(match[1], match[2], defaultPID, hasDefaultPID)
	if err != nil {
		return resumedTraceEvent{}, err
	}

	durationUS, hasDuration, err := parseOptionalDuration(match[7])
	if err != nil {
		return resumedTraceEvent{}, err
	}

	result := strings.TrimSpace(match[6])
	return resumedTraceEvent{
		pid:         pid,
		syscall:     match[4],
		argsSuffix:  match[5],
		durationUS:  durationUS,
		hasDuration: hasDuration,
		result:      result,
		errno:       parseErrno(result),
	}, nil
}

func parsePID(pidRaw, numericPIDRaw string, defaultPID int64, hasDefaultPID bool) (int64, error) {
	if pidRaw == "" {
		pidRaw = numericPIDRaw
	}
	if pidRaw == "" && hasDefaultPID {
		return defaultPID, nil
	}
	if pidRaw == "" {
		return 0, fmt.Errorf("missing pid")
	}
	pid, err := strconv.ParseInt(pidRaw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid pid %q: %w", pidRaw, err)
	}
	return pid, nil
}

func parseOptionalDuration(raw string) (int64, bool, error) {
	if raw == "" {
		return 0, false, nil
	}
	durationUS, err := parseDurationUS(raw)
	if err != nil {
		return 0, false, fmt.Errorf("invalid duration %q: %w", raw, err)
	}
	return durationUS, true, nil
}

func parseErrno(result string) string {
	if errnoMatch := errnoRE.FindStringSubmatch(result); errnoMatch != nil {
		return errnoMatch[1]
	}
	return ""
}

func finishResumedSyscall(unfinished unfinishedTraceEvent, resumed resumedTraceEvent) (traceEvent, error) {
	if unfinished.pid != resumed.pid {
		return traceEvent{}, fmt.Errorf("resumed pid %d does not match unfinished pid %d", resumed.pid, unfinished.pid)
	}
	if unfinished.syscall != resumed.syscall {
		return traceEvent{}, fmt.Errorf("resumed syscall %q does not match unfinished syscall %q", resumed.syscall, unfinished.syscall)
	}
	return traceEvent{
		timestampUS: unfinished.timestampUS,
		pid:         unfinished.pid,
		syscall:     unfinished.syscall,
		args:        combineResumedArgs(unfinished.argsPrefix, resumed.argsSuffix),
		durationUS:  resumed.durationUS,
		hasDuration: resumed.hasDuration,
		result:      resumed.result,
		errno:       resumed.errno,
	}, nil
}

func combineResumedArgs(prefix, suffix string) string {
	if suffix == "" {
		return strings.TrimRight(prefix, " \t")
	}
	return prefix + suffix
}

func epochToUTC(raw string) (string, bool) {
	timestampUS, err := epochToMicroseconds(raw)
	if err != nil {
		return "", false
	}
	return time.Unix(0, timestampUS*1_000).UTC().Format("2006-01-02T15:04:05.000000Z"), true
}

func epochToMicroseconds(raw string) (int64, error) {
	return parseSecondsToMicroseconds(raw)
}

func parseSecondsToMicroseconds(raw string) (int64, error) {
	secRaw, fracRaw, ok := strings.Cut(raw, ".")
	if !ok || secRaw == "" || fracRaw == "" {
		return 0, fmt.Errorf("expected seconds.microseconds")
	}

	sec, err := strconv.ParseInt(secRaw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid seconds: %w", err)
	}

	usec, err := parseMicroseconds(fracRaw)
	if err != nil {
		return 0, err
	}

	return sec*1_000_000 + usec, nil
}

func parseMicroseconds(raw string) (int64, error) {
	if raw == "" {
		return 0, fmt.Errorf("missing microseconds")
	}
	if len(raw) > 6 {
		return 0, fmt.Errorf("microseconds has %d digits, want at most 6", len(raw))
	}
	for len(raw) < 6 {
		raw += "0"
	}
	usec, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid microseconds: %w", err)
	}
	return usec, nil
}

func parseDurationUS(raw string) (int64, error) {
	return parseSecondsToMicroseconds(raw)
}
