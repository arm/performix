// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package systeminfo

import (
	"fmt"
	"os/exec"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestResolveAccountSidZeroLengths(t *testing.T) {
	origLookup := lookupAccountSidAPI
	defer func() { lookupAccountSidAPI = origLookup }()

	call := 0
	// Simulate LookupAccountSid returning zero lengths and no name/domain
	// dependency injection
	lookupAccountSidAPI = func(systemName *uint16, sid *windows.SID, name *uint16, cchName *uint32, domain *uint16, cchDomain *uint32, peUse *uint32) error {
		call++
		if call == 1 {
			if cchName == nil || cchDomain == nil {
				t.Fatalf("length pointers missing on probe call")
			}
			*cchName = 0
			*cchDomain = 0
			return windows.ERROR_NONE_MAPPED
		}
		if name != nil || domain != nil {
			t.Fatalf("expected nil buffers when reported length is zero")
		}
		return windows.ERROR_NONE_MAPPED
	}

	got := resolveAccountSid(&windows.SID{})
	if got != "" {
		t.Fatalf("expected empty username, got %q", got)
	}
	if call != 2 {
		t.Fatalf("expected 2 LookupAccountSid calls, got %d", call)
	}
}

func TestResolveAccountSidFormatsDomainAndUser(t *testing.T) {
	origLookup := lookupAccountSidAPI
	defer func() { lookupAccountSidAPI = origLookup }()

	call := 0
	lookupAccountSidAPI = func(systemName *uint16, sid *windows.SID, name *uint16, cchName *uint32, domain *uint16, cchDomain *uint32, peUse *uint32) error {
		call++
		switch call {
		case 1:
			if cchName == nil || cchDomain == nil {
				t.Fatalf("length pointers missing on probe call")
			}
			*cchName = 5   // "user" + null
			*cchDomain = 7 // "DOMAIN" + null
			return windows.ERROR_INSUFFICIENT_BUFFER
		case 2:
			if name == nil || domain == nil {
				t.Fatalf("expected allocated buffers on second call")
			}
			writeUTF16String(name, []uint16{'u', 's', 'e', 'r', 0})
			writeUTF16String(domain, []uint16{'D', 'O', 'M', 'A', 'I', 'N', 0})
			return nil
		default:
			t.Fatalf("unexpected LookupAccountSid call %d", call)
		}
		return nil
	}

	got := resolveAccountSid(&windows.SID{})
	if got != `DOMAIN\user` {
		t.Fatalf("expected DOMAIN\\user, got %q", got)
	}
	if call != 2 {
		t.Fatalf("expected 2 LookupAccountSid calls, got %d", call)
	}
}

func writeUTF16String(dst *uint16, data []uint16) {
	if dst == nil {
		return
	}
	buf := unsafe.Slice(dst, len(data))
	copy(buf, data)
}

func TestWindowsSystemInfoListProcessesDetectsRealProcess(t *testing.T) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", "Start-Sleep -Seconds 10")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start helper process: %v", err)
	}
	defer cmd.Process.Kill()

	wantPid := int32(cmd.Process.Pid)
	found := false

	// Try a few times to give the process time to appear in snapshots.
	for attempt := 0; attempt < 5; attempt++ {
		si := &WindowsSystemInfo{}
		procs, err := si.ListProcesses()
		if err != nil {
			t.Fatalf("ListProcesses error: %v", err)
		}
		for _, p := range procs {
			if p.Pid == wantPid {
				found = true
				assert.Equal(t, p.CmdLine, "powershell -NoProfile -NonInteractive -Command \"Start-Sleep -Seconds 10\"")
				assert.Equal(t, p.Name, "powershell.exe")
				fmt.Printf("process is: %v\n", p)
				break
			}
		}
		if found {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !found {
		t.Fatalf("expected to find pid %d in process list", wantPid)
	}
}

func TestLookupProcessOwner(t *testing.T) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", "Start-Sleep -Seconds 10")
	err := cmd.Start()
	require.NoError(t, err)
	defer cmd.Process.Kill()

	owner := lookupProcessOwner(uint32(cmd.Process.Pid))
	require.NotEmpty(t, owner)
}

func TestGetCmdLineWMI(t *testing.T) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", "Start-Sleep -Seconds 10")
	err := cmd.Start()
	require.NoError(t, err)
	defer cmd.Process.Kill()

	cmdline, err := getCmdLineWMI(uint32(cmd.Process.Pid))
	require.NoError(t, err)
	require.Equal(t, cmdline, "powershell -NoProfile -NonInteractive -Command \"Start-Sleep -Seconds 10\"")
}

func TestGetKernelVersion(t *testing.T) {
	si := &WindowsSystemInfo{}
	kernelVersion, err := si.GetKernelVersion()
	require.NoError(t, err)
	require.NotEmpty(t, kernelVersion)
}

func TestGetOSDescription(t *testing.T) {
	si := &WindowsSystemInfo{}
	desc, err := si.GetOSDescription()
	require.NoError(t, err)
	require.NotEqual(t, "", desc)
}

func TestGetCPUTopology(t *testing.T) {
	si := &WindowsSystemInfo{}
	cpuTopology, err := si.GetCPUTopology()
	require.NoError(t, err)
	require.NotEqual(t, "", cpuTopology.PrimaryCPUName)
	require.Greater(t, len(cpuTopology.CPUs), 0)
	require.Greater(t, len(cpuTopology.ClusterInfo), 0)

	for _, cpu := range cpuTopology.CPUs {
		require.Less(t, cpu.CoreNumber, uint32(len(cpuTopology.CPUs)))
		require.Less(t, cpu.ClusterID, uint32(len(cpuTopology.ClusterInfo)))
		require.Equal(t, "", cpu.Midr)
		require.Equal(t, cpuTopology.PrimaryCPUName, cpu.Name)
	}
}
