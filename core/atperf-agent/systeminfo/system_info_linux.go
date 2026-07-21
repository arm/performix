// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build linux && !android

package systeminfo

import (
	"bufio"
	"fmt"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/afero"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

var osDescriptionPattern = regexp.MustCompile(`(?m)^PRETTY_NAME="(?P<description>.+)"$`)

type userResolver func(string) (string, error)

type LinuxSystemInfo struct {
	fs          afero.Fs
	ur          userResolver
	cpuTopology CPUTopologyProvider
}

// ListProcesses scans the /proc/ filesystem and parses its entries. It returns a
// slice of ProcessInfo structs, one entry for each valid process in /proc/.
// This produces a best-effort list. Any unreadable info is left blank.
func (si *LinuxSystemInfo) ListProcesses() ([]ProcessInfo, error) {
	entries, err := afero.ReadDir(si.fs, "/proc")
	if err != nil {
		return nil, message.New(message.AgentGrpcserverApiTargetAgentListProcesses).WithCause(err)
	}

	// Cache users as we resolve them
	uidToUser := map[string]string{}

	processes := make([]ProcessInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pidStr := entry.Name()
		pid, err := strconv.ParseInt(pidStr, 10, 32)
		if err != nil {
			// Only 32-bit integer directories are process directories
			continue
		}

		name, uidStr, state := parseStatus(si.fs, pidStr)

		// Only include running (R) or sleeping (S) processes
		if state != "R" && state != "S" {
			continue
		}

		username := ""
		if cached, ok := uidToUser[uidStr]; ok {
			username = cached
		} else {
			if usr, err := si.ur(uidStr); err == nil {
				username = usr
			}
			uidToUser[uidStr] = username
		}

		if name == "" {
			// Name not found in /status, try /comm
			name = parseComm(si.fs, pidStr)
		}

		processes = append(processes, ProcessInfo{
			Pid:     int32(pid),
			User:    username,
			CmdLine: parseCmdline(si.fs, pidStr),
			Name:    name,
		})
	}

	sort.Slice(processes, func(i, j int) bool {
		return processes[i].Pid < processes[j].Pid
	})

	return processes, nil
}

// parseStatus returns the process name, uid and state from /proc/<pid>/status.
// Empty strings are returned if /status can't be parsed.
func parseStatus(fs afero.Fs, pidDir string) (string, string, string) {
	f, err := fs.Open(filepath.Join("/proc", pidDir, "status"))
	if err != nil {
		return "", "", ""
	}
	defer f.Close()

	name, uid, state := "", "", ""

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "Name:"):
			name = strings.TrimSpace(line[len("Name:"):])
		case strings.HasPrefix(line, "Uid:"):
			fields := strings.Fields(line[len("Uid:"):])
			if len(fields) > 0 {
				uid = fields[0]
			}
		case strings.HasPrefix(line, "State:"):
			rest := strings.TrimSpace(line[len("State:"):])
			if rest != "" {
				state = rest[:1]
			}
		}
		if name != "" && uid != "" && state != "" {
			break
		}
	}
	return name, uid, state
}

// parseComm returns the process name from /proc/<pid>/comm.
// An empty string is returned if /comm can't be parsed.
func parseComm(fs afero.Fs, pidDir string) string {
	b, err := afero.ReadFile(fs, filepath.Join("/proc", pidDir, "comm"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// parseCmdline returns the process command line from /proc/<pid>/cmdline.
// An empty string is returned if /cmdline can't be parsed.
func parseCmdline(fs afero.Fs, pidDir string) string {
	b, err := afero.ReadFile(fs, filepath.Join("/proc", pidDir, "cmdline"))
	if err != nil {
		return ""
	}
	parts := strings.FieldsFunc(string(b), func(r rune) bool { return r == 0 })
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

// GetKernelVersion uses the uname command to retrieve the kernel version.
func (*LinuxSystemInfo) GetKernelVersion() (string, error) {
	out, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func NewSystemInfo() SystemInfo {
	ur := func(uid string) (string, error) {
		usr, err := user.LookupId(uid)
		if usr == nil {
			return "", err
		}
		return usr.Username, err
	}

	return NewSystemInfoWithOpts(afero.NewOsFs(), ur)
}

func NewSystemInfoWithOpts(fs afero.Fs, ur userResolver) SystemInfo {
	return &LinuxSystemInfo{fs: fs, ur: ur, cpuTopology: NewSysfsCPUTopologyProvider(fs)}
}

func (si *LinuxSystemInfo) GetCPUTopology() (CPUTopology, error) {
	return si.cpuTopology.GetCPUTopology()
}

func (si *LinuxSystemInfo) GetOSDescription() (string, error) {
	bytes, err := afero.ReadFile(si.fs, filepath.Join("/etc/os-release"))
	if err != nil {
		return "", fmt.Errorf("failed to read /etc/os-release: %v", err)
	}
	contents := string(bytes)

	desc, err := findMatch(osDescriptionPattern, contents, "description")
	if err != nil {
		return "", err
	}

	return desc, nil
}

func findMatch(pattern *regexp.Regexp, body string, groupName string) (string, error) {
	matches := pattern.FindStringSubmatch(body)
	if len(matches) == 0 {
		return "", fmt.Errorf("'%v' did not match any substring of '%v'", pattern.String(), body)
	}
	index := pattern.SubexpIndex(groupName)
	if index == -1 {
		return "", fmt.Errorf("capturing group %v not defined in pattern %v", groupName, pattern.String())
	}
	return matches[index], nil
}
