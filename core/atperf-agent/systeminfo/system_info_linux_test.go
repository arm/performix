// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build linux && !android

// linux_process_manager.go
// Build on Linux
package systeminfo

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

const mockOsRelease = `PRETTY_NAME="Ubuntu 22.04.5 LTS"
NAME="Ubuntu"
VERSION_ID="22.04"
VERSION="22.04.5 LTS"
VERSION_CODENAME=jammy
ID=ubuntu
ID_LIKE=debian
HOME_URL="https://www.ubuntu.com/"
SUPPORT_URL="https://help.ubuntu.com/"
BUG_REPORT_URL="https://bugs.launchpad.net/ubuntu/"
PRIVACY_POLICY_URL="https://www.ubuntu.com/legal/terms-and-policies/privacy-policy"
UBUNTU_CODENAME=jammy`

func TestGetKernelVersion(t *testing.T) {
	info := &LinuxSystemInfo{}

	t.Run("GetKernelVersion succeeds", func(t *testing.T) {
		version, err := info.GetKernelVersion()
		assert.NoError(t, err)
		assert.NotEmpty(t, version)
	})
}

func TestListProcesses(t *testing.T) {
	fakeResolver := func(uid string) (string, error) {
		switch uid {
		case "1000":
			return "alice", nil
		case "0":
			return "root", nil
		default:
			return "", fmt.Errorf("not found")
		}
	}

	tests := []struct {
		name    string
		setupFs func(t *testing.T) afero.Fs
		want    []ProcessInfo
		wantErr bool
	}{
		{
			name: "Sorted by PID and populates Name/User. Keeps R/S states",
			setupFs: func(t *testing.T) afero.Fs {
				fs := afero.NewMemMapFs()

				if err := fs.MkdirAll("/proc/456", perms.LocalDirPerm); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
				if err := afero.WriteFile(fs, "/proc/456/status",
					[]byte("Name:\tprog2\nUid:\t0\t0\t0\t0\nState:\tS (sleeping)\n"), perms.LocalFilePerm); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
				if err := afero.WriteFile(fs, "/proc/456/cmdline",
					[]byte("prog2\x00arg1\x00arg2\x00"), perms.LocalFilePerm); err != nil {
					t.Fatalf("setup failed: %v", err)
				}

				if err := fs.MkdirAll("/proc/123", perms.LocalDirPerm); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
				if err := afero.WriteFile(fs, "/proc/123/status",
					[]byte("Name:\tprog1\nUid:\t1000\t1000\t1000\t1000\nState:\tR (running)\n"), perms.LocalFilePerm); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
				if err := afero.WriteFile(fs, "/proc/123/cmdline",
					[]byte("prog1\x00arg1\x00arg2\x00"), perms.LocalFilePerm); err != nil {
					t.Fatalf("setup failed: %v", err)
				}

				return fs
			},
			// Expect ascending by PID
			want: []ProcessInfo{
				{Pid: 123, CmdLine: "prog1 arg1 arg2", Name: "prog1", User: "alice"},
				{Pid: 456, CmdLine: "prog2 arg1 arg2", Name: "prog2", User: "root"},
			},
		},
		{
			name: "Keeps entries even with missing fields",
			setupFs: func(t *testing.T) afero.Fs {
				fs := afero.NewMemMapFs()

				// PID 111: status has only State (no Name/Uid), name comes from /comm; empty cmdline
				if err := fs.MkdirAll("/proc/111", perms.LocalDirPerm); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
				if err := afero.WriteFile(fs, "/proc/111/status",
					[]byte("State:\tS (sleeping)\n"), perms.LocalFilePerm); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
				if err := afero.WriteFile(fs, "/proc/111/comm",
					[]byte("prog123\n"), perms.LocalFilePerm); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
				if err := afero.WriteFile(fs, "/proc/111/cmdline",
					[]byte("\x00\x00"), perms.LocalFilePerm); err != nil {
					t.Fatalf("setup failed: %v", err)
				}

				// PID 222: status has Name but no Uid; has State R; cmdline present
				if err := fs.MkdirAll("/proc/222", perms.LocalDirPerm); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
				if err := afero.WriteFile(fs, "/proc/222/status",
					[]byte("Name:\thasname\nState:\tR (running)\n"), perms.LocalFilePerm); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
				if err := afero.WriteFile(fs, "/proc/222/cmdline",
					[]byte("progX\x00--flag\x00"), perms.LocalFilePerm); err != nil {
					t.Fatalf("setup failed: %v", err)
				}

				// PID 333: dir exists but no files at all
				if err := fs.MkdirAll("/proc/333", perms.LocalDirPerm); err != nil {
					t.Fatalf("setup failed: %v", err)
				}

				return fs
			},
			want: []ProcessInfo{
				{Pid: 111, CmdLine: "", Name: "prog123", User: ""},
				{Pid: 222, CmdLine: "progX --flag", Name: "hasname", User: ""},
			},
		},
		{
			name: "Filters out non-R/S and missing State",
			setupFs: func(t *testing.T) afero.Fs {
				fs := afero.NewMemMapFs()

				if err := fs.MkdirAll("/proc/700", perms.LocalDirPerm); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
				if err := afero.WriteFile(fs, "/proc/700/status",
					[]byte("Name:\ta\nUid:\t0\t0\t0\t0\nState:\tD (disk sleep)\n"), perms.LocalFilePerm); err != nil {
					t.Fatalf("setup failed: %v", err)
				}

				if err := fs.MkdirAll("/proc/800", perms.LocalDirPerm); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
				if err := afero.WriteFile(fs, "/proc/800/status",
					[]byte("Name:\tb\nUid:\t0\t0\t0\t0\nState:\tZ (zombie)\n"), perms.LocalFilePerm); err != nil {
					t.Fatalf("setup failed: %v", err)
				}

				if err := fs.MkdirAll("/proc/900", perms.LocalDirPerm); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
				if err := afero.WriteFile(fs, "/proc/900/status",
					[]byte("Name:\tc\nUid:\t1000\t1000\t1000\t1000\n"), perms.LocalFilePerm); err != nil {
					t.Fatalf("setup failed: %v", err)
				}

				return fs
			},
			want: []ProcessInfo{},
		},
		{
			name: "Fails with missing /proc",
			setupFs: func(t *testing.T) afero.Fs {
				return afero.NewMemMapFs()
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			si := NewSystemInfoWithOpts(tt.setupFs(t), fakeResolver)

			got, err := si.ListProcesses()
			if tt.wantErr {
				var msgErr message.Message
				ok := errors.As(err, &msgErr)
				assert.True(t, ok)
				assert.Equal(t, message.AgentGrpcserverApiTargetAgentListProcesses, msgErr.Code())
				assert.Error(t, msgErr.Unwrap())
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("unexpected result")
			}
		})
	}
}

func TestGetOSInfo(t *testing.T) {
	fakeResolver := func(uid string) (string, error) {
		return "", nil
	}
	t.Run("parses /etc/os-release correctly", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		assert.NoError(t, fs.Mkdir("/etc", perms.LocalDirPerm))
		assert.NoError(t, afero.WriteFile(fs, "/etc/os-release",
			[]byte(mockOsRelease), perms.LocalFilePerm))

		si := NewSystemInfoWithOpts(fs, fakeResolver)
		result, err := si.GetOSDescription()
		assert.NoError(t, err)

		expected := "Ubuntu 22.04.5 LTS"
		assert.Equal(t, expected, result)
	})
	t.Run("returns error on file not existing", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		// Don't create `os-release` file

		si := NewSystemInfoWithOpts(fs, fakeResolver)
		_, err := si.GetOSDescription()
		assert.ErrorContains(t, err, "failed to read /etc/os-release")
	})
	t.Run("returns error on parse failure", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		assert.NoError(t, fs.Mkdir("/etc", perms.LocalDirPerm))
		assert.NoError(t, afero.WriteFile(fs, "/etc/os-release",
			[]byte("good luck parsing this"), perms.LocalFilePerm))

		si := NewSystemInfoWithOpts(fs, fakeResolver)
		_, err := si.GetOSDescription()
		assert.ErrorContains(t, err, "did not match any substring of 'good luck parsing this'")
	})
}

func TestGetCPUTopology(t *testing.T) {
	fakeResolver := func(uid string) (string, error) {
		return "", nil
	}

	t.Run("builds topology from sysfs and cpuinfo", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		for _, path := range []string{
			"/proc",
			"/sys",
			"/sys/devices",
			"/sys/devices/system",
			"/sys/devices/system/cpu",
			"/sys/devices/system/cpu/cpu0",
			"/sys/devices/system/cpu/cpu0/topology",
			"/sys/devices/system/cpu/cpu0/regs",
			"/sys/devices/system/cpu/cpu0/regs/identification",
			"/sys/devices/system/cpu/cpu1",
			"/sys/devices/system/cpu/cpu1/topology",
			"/sys/devices/system/cpu/cpu1/regs",
			"/sys/devices/system/cpu/cpu1/regs/identification",
		} {
			assert.NoError(t, fs.MkdirAll(path, perms.LocalDirPerm))
		}

		assert.NoError(t, afero.WriteFile(fs, "/proc/cpuinfo", []byte(`processor	: 0
model name	: Neoverse-V2
processor	: 1
model name	: Neoverse-V2
`), perms.LocalFilePerm))
		assert.NoError(t, afero.WriteFile(fs, "/sys/devices/system/cpu/cpu0/topology/cluster_id", []byte("0\n"), perms.LocalFilePerm))
		assert.NoError(t, afero.WriteFile(fs, "/sys/devices/system/cpu/cpu1/topology/cluster_id", []byte("0\n"), perms.LocalFilePerm))
		assert.NoError(t, afero.WriteFile(fs, "/sys/devices/system/cpu/cpu0/regs/identification/midr_el1", []byte("0x00000000410fd4f1\n"), perms.LocalFilePerm))
		assert.NoError(t, afero.WriteFile(fs, "/sys/devices/system/cpu/cpu1/regs/identification/midr_el1", []byte("0x00000000410fd4f1\n"), perms.LocalFilePerm))

		si := NewSystemInfoWithOpts(fs, fakeResolver)
		topology, err := si.GetCPUTopology()
		assert.NoError(t, err)
		assert.Equal(t, "Neoverse-V2", topology.PrimaryCPUName)
		assert.Equal(t, []ClusterDescription{{ClusterID: 0, Name: "Cluster 0"}}, topology.ClusterInfo)
		assert.Equal(t, []CPUDescription{
			{CoreNumber: 0, ClusterID: 0, Midr: "0x410fd4f1", Name: "Neoverse-V2"},
			{CoreNumber: 1, ClusterID: 0, Midr: "0x410fd4f1", Name: "Neoverse-V2"},
		}, topology.CPUs)
	})

	t.Run("continues when cpuinfo is unreadable", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		for _, path := range []string{
			"/sys",
			"/sys/devices",
			"/sys/devices/system",
			"/sys/devices/system/cpu",
			"/sys/devices/system/cpu/cpu0",
			"/sys/devices/system/cpu/cpu0/topology",
			"/sys/devices/system/cpu/cpu0/regs",
			"/sys/devices/system/cpu/cpu0/regs/identification",
		} {
			assert.NoError(t, fs.MkdirAll(path, perms.LocalDirPerm))
		}

		assert.NoError(t, afero.WriteFile(fs, "/sys/devices/system/cpu/cpu0/topology/cluster_id", []byte("7\n"), perms.LocalFilePerm))
		assert.NoError(t, afero.WriteFile(fs, "/sys/devices/system/cpu/cpu0/regs/identification/midr_el1", []byte("0x413fd0c1\n"), perms.LocalFilePerm))

		si := NewSystemInfoWithOpts(fs, fakeResolver)
		topology, err := si.GetCPUTopology()
		assert.NoError(t, err)
		assert.Equal(t, "Neoverse-N1", topology.PrimaryCPUName)
		assert.Equal(t, []CPUDescription{
			{CoreNumber: 0, ClusterID: 7, Midr: "0x413fd0c1", Name: "Neoverse-N1"},
		}, topology.CPUs)
		assert.Equal(t, []ClusterDescription{{ClusterID: 7, Name: "Cluster 0"}}, topology.ClusterInfo)
	})

	t.Run("assigns a synthetic cluster when cluster information is unreadable", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		for _, path := range []string{
			"/proc",
			"/sys",
			"/sys/devices",
			"/sys/devices/system",
			"/sys/devices/system/cpu",
			"/sys/devices/system/cpu/cpu0",
			"/sys/devices/system/cpu/cpu0/topology",
			"/sys/devices/system/cpu/cpu0/regs",
			"/sys/devices/system/cpu/cpu0/regs/identification",
			"/sys/devices/system/cpu/cpu1",
			"/sys/devices/system/cpu/cpu1/topology",
			"/sys/devices/system/cpu/cpu1/regs",
			"/sys/devices/system/cpu/cpu1/regs/identification",
		} {
			assert.NoError(t, fs.MkdirAll(path, perms.LocalDirPerm))
		}

		assert.NoError(t, afero.WriteFile(fs, "/proc/cpuinfo", []byte(`processor	: 0
model name	: Neoverse-V2
processor	: 1
model name	: Neoverse-V2
`), perms.LocalFilePerm))
		assert.NoError(t, afero.WriteFile(fs, "/sys/devices/system/cpu/cpu0/topology/cluster_id", []byte("4\n"), perms.LocalFilePerm))
		assert.NoError(t, afero.WriteFile(fs, "/sys/devices/system/cpu/cpu0/regs/identification/midr_el1", []byte("0x00000000410fd4f1\n"), perms.LocalFilePerm))
		assert.NoError(t, afero.WriteFile(fs, "/sys/devices/system/cpu/cpu1/regs/identification/midr_el1", []byte("0x00000000410fd4f1\n"), perms.LocalFilePerm))

		si := NewSystemInfoWithOpts(fs, fakeResolver)
		topology, err := si.GetCPUTopology()
		assert.NoError(t, err)
		assert.Equal(t, "Neoverse-V2", topology.PrimaryCPUName)
		assert.Equal(t, []CPUDescription{
			{CoreNumber: 0, ClusterID: 4, Midr: "0x410fd4f1", Name: "Neoverse-V2"},
			{CoreNumber: 1, ClusterID: 4294967295, Midr: "0x410fd4f1", Name: "Neoverse-V2"},
		}, topology.CPUs)
		assert.Equal(t, []ClusterDescription{
			{ClusterID: 4, Name: "Cluster 0"},
			{ClusterID: 4294967295, Name: "Cluster 1"},
		}, topology.ClusterInfo)
	})

}

func TestResolveCPUName(t *testing.T) {
	t.Run("uses MIDR-derived cpuid when available", func(t *testing.T) {
		name := resolveCPUName(cpuInfo{
			modelName:      "Generic CPU",
			cpuImplementer: "0x41",
			cpuPart:        "0xd0c",
		}, "0x413fd4f1")
		assert.Equal(t, "Neoverse-V2", name)
	})

	t.Run("falls back to implementer and part before model name", func(t *testing.T) {
		name := resolveCPUName(cpuInfo{
			modelName:      "Generic CPU",
			cpuImplementer: "0x41",
			cpuPart:        "0xd4f",
		}, "")
		assert.Equal(t, "Neoverse-V2", name)
	})

	t.Run("falls back to model name when cpuid lookup data is unavailable", func(t *testing.T) {
		name := resolveCPUName(cpuInfo{
			modelName: "Vendor CPU String",
		}, "")
		assert.Equal(t, "Vendor CPU String", name)
	})

	t.Run("falls back to unknown cpu when no name source is available", func(t *testing.T) {
		name := resolveCPUName(cpuInfo{}, "")
		assert.Equal(t, "Unknown CPU", name)
	})
}
