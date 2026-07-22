// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingADBRunner struct {
	calls     [][]string
	err       error
	responses map[string]adbRunResult
}

type adbRunResult struct {
	stdout string
	stderr string
	err    error
}

func (r *recordingADBRunner) Run(args ...string) (string, string, error) {
	copied := append([]string(nil), args...)
	r.calls = append(r.calls, copied)
	if response, ok := r.responses[strings.Join(args, "\x00")]; ok {
		return response.stdout, response.stderr, response.err
	}
	if r.err != nil {
		return "", "", r.err
	}
	return "ok", "", nil
}

func adbRunKey(args ...string) string {
	return strings.Join(args, "\x00")
}

func TestADBCommandRunner(t *testing.T) {
	deviceIP := "android-target.invalid:5555"
	runner := &recordingADBRunner{}
	client := newADBClientWithRunner("device-123", &deviceIP, runner)
	cmdRunner := &ADBCommandRunner{client: client}

	stdout, stderr, err := cmdRunner.RunCommand("uname -a")
	require.NoError(t, err)
	assert.Equal(t, "ok", stdout)
	assert.Empty(t, stderr)
	require.Len(t, runner.calls, 3)
	assert.Equal(t, []string{"devices"}, runner.calls[0])
	assert.Equal(t, []string{"connect", deviceIP}, runner.calls[1])
	assert.Equal(t, []string{"-s", "device-123", "shell", "uname -a"}, runner.calls[2])
}

func TestADBCommandRunnerSkipsConnectWhenDeviceIsAlreadyListed(t *testing.T) {
	deviceIP := "android-target.invalid:5555"
	runner := &recordingADBRunner{
		responses: map[string]adbRunResult{
			adbRunKey("devices"): {stdout: "List of devices attached\ndevice-123\tdevice\n"},
		},
	}
	client := newADBClientWithRunner("device-123", &deviceIP, runner)
	cmdRunner := &ADBCommandRunner{client: client}

	_, _, err := cmdRunner.RunCommand("uname -a")
	require.NoError(t, err)
	require.Len(t, runner.calls, 2)
	assert.Equal(t, []string{"devices"}, runner.calls[0])
	assert.Equal(t, []string{"-s", "device-123", "shell", "uname -a"}, runner.calls[1])
}

func TestADBCommandRunnerUsesLocallyVisibleDeviceWithoutDeviceIP(t *testing.T) {
	runner := &recordingADBRunner{
		responses: map[string]adbRunResult{
			adbRunKey("devices"): {stdout: "List of devices attached\ndevice-123\tdevice\n"},
		},
	}
	client := newADBClientWithRunner("device-123", nil, runner)
	cmdRunner := &ADBCommandRunner{client: client}

	_, _, err := cmdRunner.RunCommand("uname -a")
	require.NoError(t, err)
	require.Len(t, runner.calls, 2)
	assert.Equal(t, []string{"devices"}, runner.calls[0])
	assert.Equal(t, []string{"-s", "device-123", "shell", "uname -a"}, runner.calls[1])
}

func TestADBCommandRunnerCachesConnectedDevice(t *testing.T) {
	runner := &recordingADBRunner{
		responses: map[string]adbRunResult{
			adbRunKey("devices"): {stdout: "List of devices attached\ndevice-123\tdevice\n"},
		},
	}
	client := newADBClientWithRunner("device-123", nil, runner)
	cmdRunner := &ADBCommandRunner{client: client}

	_, _, err := cmdRunner.RunCommand("uname -a")
	require.NoError(t, err)
	_, _, err = cmdRunner.RunCommand("id")
	require.NoError(t, err)

	require.Len(t, runner.calls, 3)
	assert.Equal(t, []string{"devices"}, runner.calls[0])
	assert.Equal(t, []string{"-s", "device-123", "shell", "uname -a"}, runner.calls[1])
	assert.Equal(t, []string{"-s", "device-123", "shell", "id"}, runner.calls[2])
}

func TestADBCommandRunnerInvalidatesCacheOnCommandError(t *testing.T) {
	commandErr := errors.New("device disconnected")
	runner := &recordingADBRunner{
		responses: map[string]adbRunResult{
			adbRunKey("devices"):                            {stdout: "List of devices attached\ndevice-123\tdevice\n"},
			adbRunKey("-s", "device-123", "shell", "false"): {err: commandErr},
		},
	}
	client := newADBClientWithRunner("device-123", nil, runner)
	cmdRunner := &ADBCommandRunner{client: client}

	_, _, err := cmdRunner.RunCommand("false")
	require.ErrorIs(t, err, commandErr)
	_, _, err = cmdRunner.RunCommand("uname -a")
	require.NoError(t, err)

	require.Len(t, runner.calls, 4)
	assert.Equal(t, []string{"devices"}, runner.calls[0])
	assert.Equal(t, []string{"-s", "device-123", "shell", "false"}, runner.calls[1])
	assert.Equal(t, []string{"devices"}, runner.calls[2])
	assert.Equal(t, []string{"-s", "device-123", "shell", "uname -a"}, runner.calls[3])
}

func TestADBCommandRunnerReturnsErrorWhenDeviceIsNotVisibleWithoutDeviceIP(t *testing.T) {
	runner := &recordingADBRunner{}
	client := newADBClientWithRunner("device-123", nil, runner)
	cmdRunner := &ADBCommandRunner{client: client}

	_, _, err := cmdRunner.RunCommand("uname -a")
	require.ErrorContains(t, err, `android device "device-123" is not visible in adb devices`)
	require.Len(t, runner.calls, 1)
	assert.Equal(t, []string{"devices"}, runner.calls[0])
}

func TestADBCommandRunnerReturnsConnectError(t *testing.T) {
	connectErr := errors.New("no adb")
	deviceIP := "android-target.invalid:5555"
	runner := &recordingADBRunner{
		responses: map[string]adbRunResult{
			adbRunKey("connect", deviceIP): {err: connectErr},
		},
	}
	client := newADBClientWithRunner("device-123", &deviceIP, runner)
	cmdRunner := &ADBCommandRunner{client: client}

	_, _, err := cmdRunner.RunCommand("uname -a")
	require.ErrorIs(t, err, connectErr)
	require.Len(t, runner.calls, 2)
	assert.Equal(t, []string{"devices"}, runner.calls[0])
	assert.Equal(t, []string{"connect", deviceIP}, runner.calls[1])
}

func TestADBCheckHealth(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		runner := &recordingADBRunner{
			responses: map[string]adbRunResult{
				adbRunKey("devices"): {stdout: "List of devices attached\ndevice-123\tdevice\n"},
			},
		}
		client := newADBClientWithRunner("device-123", nil, runner)

		err := client.CheckHealth()

		require.NoError(t, err)
		require.Len(t, runner.calls, 2)
		assert.Equal(t, []string{"devices"}, runner.calls[0])
		assert.Equal(t, []string{"-s", "device-123", "get-state"}, runner.calls[1])
	})

	t.Run("wraps adb error", func(t *testing.T) {
		healthErr := errors.New("adb get-state failed")
		runner := &recordingADBRunner{
			responses: map[string]adbRunResult{
				adbRunKey("devices"):                       {stdout: "List of devices attached\ndevice-123\tdevice\n"},
				adbRunKey("-s", "device-123", "get-state"): {err: healthErr},
			},
		}
		client := newADBClientWithRunner("device-123", nil, runner)

		err := client.CheckHealth()

		require.ErrorIs(t, err, healthErr)
		require.ErrorContains(t, err, "health check for Android target failed")
		require.Len(t, runner.calls, 2)
		assert.Equal(t, []string{"devices"}, runner.calls[0])
		assert.Equal(t, []string{"-s", "device-123", "get-state"}, runner.calls[1])
	})
}

func TestADBClient(t *testing.T) {
	conn := NewADBClient("device-123", nil)

	require.IsType(t, &ADBCommandRunner{}, conn.CommandRunner())
	require.IsType(t, &ADBTargetFilesystem{}, conn.Filesystem())
}

func TestADBTargetFilesystemFileStat(t *testing.T) {
	t.Run("file", func(t *testing.T) {
		runner := &recordingADBRunner{
			responses: map[string]adbRunResult{
				adbRunKey("devices"): {stdout: "List of devices attached\ndevice-123\tdevice\n"},
				adbRunKey("-s", "device-123", "shell", "stat -c '%s %A' '/data/local/tmp/file'"): {
					stdout: "42 -rw-r--r--\n",
				},
			},
		}
		fs := newADBClientWithRunner("device-123", nil, runner).Filesystem()

		info, err := fs.FileStat("/data/local/tmp/file")

		require.NoError(t, err)
		assert.Equal(t, "file", info.Name())
		assert.Equal(t, int64(42), info.Size())
		assert.False(t, info.IsDir())
		require.Len(t, runner.calls, 2)
		assert.Equal(t, []string{"devices"}, runner.calls[0])
		assert.Equal(t, []string{"-s", "device-123", "shell", "stat -c '%s %A' '/data/local/tmp/file'"}, runner.calls[1])
	})

	t.Run("directory", func(t *testing.T) {
		runner := &recordingADBRunner{
			responses: map[string]adbRunResult{
				adbRunKey("devices"): {stdout: "List of devices attached\ndevice-123\tdevice\n"},
				adbRunKey("-s", "device-123", "shell", "stat -c '%s %A' '/data/local/tmp/dir'"): {
					stdout: "4096 drwxr-xr-x\n",
				},
			},
		}
		fs := newADBClientWithRunner("device-123", nil, runner).Filesystem()

		info, err := fs.FileStat("/data/local/tmp/dir")

		require.NoError(t, err)
		assert.Equal(t, "dir", info.Name())
		assert.Equal(t, int64(4096), info.Size())
		assert.True(t, info.IsDir())
	})

	t.Run("parse error", func(t *testing.T) {
		runner := &recordingADBRunner{
			responses: map[string]adbRunResult{
				adbRunKey("devices"): {stdout: "List of devices attached\ndevice-123\tdevice\n"},
				adbRunKey("-s", "device-123", "shell", "stat -c '%s %A' '/data/local/tmp/file'"): {
					stdout: "not-a-stat-result\n",
				},
			},
		}
		fs := newADBClientWithRunner("device-123", nil, runner).Filesystem()

		_, err := fs.FileStat("/data/local/tmp/file")

		require.ErrorContains(t, err, "could not parse adb stat output")
	})
}

func TestADBTargetFilesystemCreateDirTree(t *testing.T) {
	runner := &recordingADBRunner{
		responses: map[string]adbRunResult{
			adbRunKey("devices"): {stdout: "List of devices attached\ndevice-123\tdevice\n"},
		},
	}
	fs := newADBClientWithRunner("device-123", nil, runner).Filesystem()

	err := fs.CreateDirTree("/data/local/tmp/apap tools", 0o700)

	require.NoError(t, err)
	require.Len(t, runner.calls, 3)
	assert.Equal(t, []string{"devices"}, runner.calls[0])
	assert.Equal(t, []string{"-s", "device-123", "shell", "mkdir -p '/data/local/tmp/apap tools'"}, runner.calls[1])
	assert.Equal(t, []string{"-s", "device-123", "shell", "chmod u=rwx,g=,o= '/data/local/tmp/apap tools'"}, runner.calls[2])
}

func TestADBTargetFilesystemRemoveDirTree(t *testing.T) {
	runner := &recordingADBRunner{
		responses: map[string]adbRunResult{
			adbRunKey("devices"): {stdout: "List of devices attached\ndevice-123\tdevice\n"},
		},
	}
	fs := newADBClientWithRunner("device-123", nil, runner).Filesystem()

	err := fs.RemoveDirTree("/data/local/tmp/apap tools")

	require.NoError(t, err)
	require.Len(t, runner.calls, 2)
	assert.Equal(t, []string{"devices"}, runner.calls[0])
	assert.Equal(t, []string{"-s", "device-123", "shell", "rm -rf '/data/local/tmp/apap tools'"}, runner.calls[1])
}

func TestADBTargetFilesystemCreateEmptyFile(t *testing.T) {
	runner := &recordingADBRunner{
		responses: map[string]adbRunResult{
			adbRunKey("devices"): {stdout: "List of devices attached\ndevice-123\tdevice\n"},
		},
	}
	fs := newADBClientWithRunner("device-123", nil, runner).Filesystem()

	err := fs.CreateEmptyFile("/data/local/tmp/apap tools/.extracted", 0o644)

	require.NoError(t, err)
	require.Len(t, runner.calls, 3)
	assert.Equal(t, []string{"devices"}, runner.calls[0])
	assert.Equal(t, []string{"-s", "device-123", "shell", "touch '/data/local/tmp/apap tools/.extracted'"}, runner.calls[1])
	assert.Equal(t, []string{"-s", "device-123", "shell", "chmod u=rw,g=r,o=r '/data/local/tmp/apap tools/.extracted'"}, runner.calls[2])
}

func TestADBTargetFilesystemCopyFromHostUsesPush(t *testing.T) {
	runner := &recordingADBRunner{
		responses: map[string]adbRunResult{
			adbRunKey("devices"): {stdout: "List of devices attached\ndevice-123\tdevice\n"},
		},
	}
	client := newADBClientWithRunner("device-123", nil, runner)
	fs := client.Filesystem()
	hostPath := filepath.Join(t.TempDir(), "tool")
	require.NoError(t, os.WriteFile(hostPath, []byte("payload"), 0o644))

	var progress []int64
	err := fs.CopyFromHost(hostPath, "/data/local/tmp/tool", []ReportProgressRequest{{
		Callback: func(received int64, max int64, when time.Time) {
			progress = append(progress, received, max)
		},
	}})

	require.NoError(t, err)
	assert.Equal(t, []int64{0, 7, 7, 7}, progress)
	require.Len(t, runner.calls, 2)
	assert.Equal(t, []string{"devices"}, runner.calls[0])
	assert.Equal(t, []string{"-s", "device-123", "push", hostPath, "/data/local/tmp/tool"}, runner.calls[1])
}

func TestADBTargetFilesystemCopyFromHostReturnsHostStatError(t *testing.T) {
	runner := &recordingADBRunner{}
	fs := newADBClientWithRunner("device-123", nil, runner).Filesystem()
	hostPath := filepath.Join(t.TempDir(), "missing-tool")

	var progress []int64
	err := fs.CopyFromHost(hostPath, "/data/local/tmp/tool", []ReportProgressRequest{{
		Callback: func(received int64, max int64, when time.Time) {
			progress = append(progress, received, max)
		},
	}})

	require.ErrorIs(t, err, os.ErrNotExist)
	assert.Empty(t, progress)
	assert.Empty(t, runner.calls)
}

func TestADBDialRejectsInvalidAddress(t *testing.T) {
	runner := &recordingADBRunner{}
	client := newADBClientWithRunner("device-123", nil, runner)

	_, err := client.Dial("tcp", "not-a-host-port")
	require.Error(t, err)
	assert.Empty(t, runner.calls)
}

func TestADBDialRemovesForwardWhenLocalDialFails(t *testing.T) {
	runner := &recordingADBRunner{
		responses: map[string]adbRunResult{
			adbRunKey("devices"): {stdout: "List of devices attached\ndevice-123\tdevice\n"},
		},
	}
	client := newADBClientWithRunner("device-123", nil, runner)

	_, err := client.Dial("tcp", "android-device.invalid:1234")

	require.Error(t, err)
	require.Len(t, runner.calls, 3)
	assert.Equal(t, []string{"devices"}, runner.calls[0])
	assert.Equal(t, []string{"-s", "device-123", "forward", runner.calls[1][3], "tcp:1234"}, runner.calls[1])
	assert.Equal(t, []string{"-s", "device-123", "forward", "--remove", runner.calls[1][3]}, runner.calls[2])
}
