// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows || linux || darwin

package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

func TestStartProcessRedirects(t *testing.T) {
	assertStream := func(t *testing.T, mode RedirectMode, expected string, pid int, sendFn func(int, StreamChunkSender) error) {
		sender := &fakeSender{ctx: context.Background()}
		if mode == Both || mode == Stream {
			require.NoError(t, sendFn(pid, sender))
			joined := bytes.Join(sender.chunks, nil)
			assert.Contains(t, string(joined), expected)
		} else {
			require.Error(t, sendFn(pid, sender))
		}
	}

	assertFile := func(t *testing.T, mode RedirectMode, expected, path string, fs afero.Fs) {
		if mode == Both || mode == File {
			require.Eventually(t, func() bool {
				data, err := afero.ReadFile(fs, path)
				return err == nil && (expected == "" || bytes.Contains(data, []byte(expected)))
			}, time.Second, 25*time.Millisecond)

			data, err := afero.ReadFile(fs, path)
			require.NoError(t, err)
			assert.Contains(t, string(data), expected)
		} else {
			_, err := afero.ReadFile(fs, path)
			require.Error(t, err)
		}
	}

	tests := []struct {
		name           string
		command        []string
		fs             func() afero.Fs
		stdoutMode     RedirectMode
		expectedStdout string
		stdoutFile     string
		stderrMode     RedirectMode
		expectedStderr string
		stderrFile     string
		stdinMode      StdinMode
		wantErr        bool
		errContains    string
	}{
		{"Mode is both success", platformEchoCommand("both"), afero.NewMemMapFs, Both, "both", "stdout.txt", Both, "", "stderr.txt", StdinNone, false, ""},
		{"Mode is stream success", platformEchoCommand("stream"), afero.NewMemMapFs, Stream, "stream", "stdout.txt", Stream, "", "stderr.txt", StdinNone, false, ""},
		{"Mode is file success", platformEchoCommand("file"), afero.NewMemMapFs, File, "", "stdout.txt", File, "", "stderr.txt", StdinNone, false, ""},
		{"Mode is none success", platformEchoCommand("none"), afero.NewMemMapFs, None, "", "stdout.txt", None, "", "stderr.txt", StdinNone, false, ""},
		{"Mode is none on stdin success", platformEchoCommand("none"), afero.NewMemMapFs, Stream, "none", "stdout.txt", None, "", "stderr.txt", StdinNone, false, ""},
		{"Mode is buffer on stdin success", platformEchoCommand("buffer"), afero.NewMemMapFs, Stream, "buffer", "stdout.txt", None, "", "stderr.txt", StdinBuffer, false, ""},
		{"Mode is non-existent on stdin fail", platformEchoCommand("stdin"), afero.NewMemMapFs, Stream, "stdin", "stdout.txt", None, "", "stderr.txt", StdinMode(-1), true, "unsupported redirect mode"},
		{"Invalid command fail", []string{"no_such_executable"}, afero.NewMemMapFs, Stream, "", "stdout.txt", Stream, "", "stderr.txt", StdinNone, true, "error running command"},
		{"File not writable fail", platformEchoCommand("hello"), func() afero.Fs { return afero.NewReadOnlyFs(afero.NewMemMapFs()) }, File, "", "stdout.txt", File, "", "stderr.txt", StdinNone, true, "setup error"},
		{"Bad paths fail", []string{"no_such_executable"}, afero.NewMemMapFs, File, "", "nonexistent/stdout.txt", File, "", "nonexistent/stderr.txt", StdinNone, true, "invalid stdout file path"},
		{"Duplicate paths fail", []string{"no_such_executable"}, afero.NewMemMapFs, File, "", "same.txt", File, "", "same.txt", StdinNone, true, "paths must be different"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			fs := tt.fs()
			pm := NewProcessManagerWithFs(fs)
			sp := &StartProcess{
				LaunchCommand: LaunchCommand{Command: tt.command},
				Stdout:        StreamRedirect{Mode: tt.stdoutMode, FilePath: tt.stdoutFile},
				Stderr:        StreamRedirect{Mode: tt.stderrMode, FilePath: tt.stderrFile},
				Stdin:         tt.stdinMode,
			}

			proc, err := pm.StartProcess(sp)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assertStream(t, tt.stdoutMode, tt.expectedStdout, proc.Pid, pm.StreamStdout)
			assertStream(t, tt.stderrMode, tt.expectedStderr, proc.Pid, pm.StreamStderr)
			assertFile(t, tt.stdoutMode, tt.expectedStdout, tt.stdoutFile, fs)
			assertFile(t, tt.stderrMode, tt.expectedStderr, tt.stderrFile, fs)
		})
	}
}

func TestExecCommandReturnsResultWhenCommandIsNotFound(t *testing.T) {
	pm := NewProcessManager()

	result, err := pm.ExecCommand(&LaunchCommand{
		Command: []string{"performix-command-that-does-not-exist"},
	})

	require.NoError(t, err)
	assert.Equal(t, CommandNotFoundExitCode, result.Rc)
	assert.Empty(t, result.Stdout)
	assert.NotEmpty(t, result.Stderr)
}

func TestValidateFilePath(t *testing.T) {
	fs := afero.NewMemMapFs()
	pm := newProcessCommon(fs)

	// Empty path
	err := pm.validateFilePath("")
	require.Error(t, err)

	// Dot path
	err = pm.validateFilePath(".")
	require.Error(t, err)

	// Non-existent directory
	err = pm.validateFilePath("nonexistent/file.txt")
	require.Error(t, err)

	// Directory is a file
	err = afero.WriteFile(fs, "notadir", []byte(""), perms.LocalFilePerm)
	require.NoError(t, err)
	err = pm.validateFilePath("notadir/file.txt")
	require.Error(t, err)

	// File already exists
	err = fs.MkdirAll("dir", perms.LocalDirPerm)
	require.NoError(t, err)
	err = afero.WriteFile(fs, "dir/existing.txt", []byte("data"), perms.LocalFilePerm)
	require.NoError(t, err)
	err = pm.validateFilePath("dir/existing.txt")
	require.Error(t, err)

	// Valid new file
	err = fs.MkdirAll("dir2", perms.LocalDirPerm)
	require.NoError(t, err)
	err = pm.validateFilePath("dir2/new.txt")
	require.NoError(t, err)
}

func TestStreamMethods(t *testing.T) {
	pm := NewProcessManagerWithFs(afero.NewMemMapFs())

	t.Run("StreamStdout and StreamStderr success", func(t *testing.T) {
		proc, err := pm.StartProcess(&StartProcess{
			LaunchCommand: LaunchCommand{Command: platformEchoCommand("mystdout")},
			Stdout:        StreamRedirect{Mode: Stream},
			Stderr:        StreamRedirect{Mode: Stream},
		})
		require.NoError(t, err)

		sender := &fakeSender{ctx: context.Background()}
		require.NoError(t, pm.StreamStdout(proc.Pid, sender))
		assert.Contains(t, string(bytes.Join(sender.chunks, nil)), "mystdout")

		sender = &fakeSender{ctx: context.Background()}
		require.NoError(t, pm.StreamStderr(proc.Pid, sender))
		assert.Equal(t, "", string(bytes.Join(sender.chunks, nil)))
	})

	t.Run("StreamStdout returns error when not configured", func(t *testing.T) {
		proc, err := pm.StartProcess(&StartProcess{
			LaunchCommand: LaunchCommand{Command: platformEchoCommand("mystdout")},
			Stdout:        StreamRedirect{Mode: File, FilePath: "stdout.txt"},
		})
		require.NoError(t, err)
		err = pm.StreamStdout(proc.Pid, &fakeSender{ctx: context.Background()})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stdout stream not available")
	})

	t.Run("StreamStdout returns error after a previous StreamStdout has finished", func(t *testing.T) {
		proc, err := pm.StartProcess(&StartProcess{
			LaunchCommand: LaunchCommand{Command: platformEchoCommand("mystdout")},
			Stdout:        StreamRedirect{Mode: Stream},
		})
		require.NoError(t, err)

		sender := &fakeSender{ctx: context.Background()}
		require.NoError(t, pm.StreamStdout(proc.Pid, sender))

		err = pm.StreamStdout(proc.Pid, sender)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stream already consumed")
	})

	t.Run("StreamStdout returns error when called concurrently with another StreamStdout", func(t *testing.T) {
		proc, err := pm.StartProcess(&StartProcess{
			LaunchCommand: LaunchCommand{Command: platformSleepCommand(1)},
			Stdout:        StreamRedirect{Mode: Stream},
		})
		require.NoError(t, err)

		sender := &fakeSender{ctx: context.Background()}

		wg := sync.WaitGroup{}
		wg.Add(2)
		errCh := make(chan error, 2)

		// First call
		go func() {
			defer wg.Done()
			err := pm.StreamStdout(proc.Pid, sender)
			errCh <- err
		}()

		// Second call
		go func() {
			defer wg.Done()
			err := pm.StreamStdout(proc.Pid, sender)
			errCh <- err
		}()

		doneCh := make(chan struct{})
		go func() {
			wg.Wait()
			close(doneCh)
		}()

		select {
		case <-doneCh:
			// Both done
		case <-time.After(15 * time.Second):
			t.Fatal("Timeout waiting for StreamStdout calls to finish")
		}

		// One should succeed, the other should fail
		// We don't know which will be first

		// Dummy initial values to ensure each is set
		success := errors.New("dummy")
		failure := errors.New("dummy")

		for i := 0; i < 2; i++ {
			err := <-errCh
			if err != nil {
				failure = err
			} else {
				success = err
			}
		}

		require.NoError(t, success)
		require.Error(t, failure)
		assert.Contains(t, failure.Error(), "stream already consumed")

		_ = pm.KillProcess(proc.Pid)
	})

	t.Run("Context cancellation returns error", func(t *testing.T) {
		proc, err := pm.StartProcess(&StartProcess{
			LaunchCommand: LaunchCommand{Command: platformEchoCommand("mystdout")},
			Stdout:        StreamRedirect{Mode: Stream},
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		sender := &fakeSender{ctx: ctx}
		err = pm.StreamStdout(proc.Pid, sender)
		require.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})
}

func TestStdinModes(t *testing.T) {
	pm := NewProcessManagerWithFs(afero.NewMemMapFs())

	t.Run("WriteToStdin succeeds if mode is buffer", func(t *testing.T) {
		proc, err := pm.StartProcess(&StartProcess{
			LaunchCommand: LaunchCommand{Command: platformReadStdinCommand()},
			Stdout:        StreamRedirect{Mode: Stream},
			Stdin:         StdinBuffer,
		})
		require.NoError(t, err)

		require.NoError(t, pm.WriteToStdin(proc.Pid, []byte("hello")))
		_, err = pm.WaitProcess(context.Background(), proc.Pid)
		require.NoError(t, err)

		sender := &fakeSender{ctx: context.Background()}
		require.NoError(t, pm.StreamStdout(proc.Pid, sender))
		assert.Contains(t, string(bytes.Join(sender.chunks, nil)), "hello")
	})

	t.Run("WriteToStdin fails if mode is none", func(t *testing.T) {
		proc, err := pm.StartProcess(&StartProcess{
			LaunchCommand: LaunchCommand{Command: platformReadStdinCommand()},
			Stdout:        StreamRedirect{Mode: Stream},
			Stdin:         StdinNone,
		})
		require.NoError(t, err)

		err = pm.WriteToStdin(proc.Pid, []byte("hello"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stdin redirection not available")
		_ = pm.KillProcess(proc.Pid)
	})
}

func TestRedirectHandleClose(t *testing.T) {
	t.Run("close is idempotent", func(t *testing.T) {
		pr, pw, err := os.Pipe()
		require.NoError(t, err)

		h := &redirectHandle{pr: pr, pw: pw}

		// First close should succeed
		require.NoError(t, h.close())

		// Second close should be a no-op, not an error or panic
		require.NoError(t, h.close())
	})

	t.Run("concurrent closes do not race or panic", func(t *testing.T) {
		pr, pw, err := os.Pipe()
		require.NoError(t, err)

		h := &redirectHandle{pr: pr, pw: pw}

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = h.close()
			}()
		}
		wg.Wait()
	})

	t.Run("returns error when pw is already closed", func(t *testing.T) {
		pr, pw, err := os.Pipe()
		require.NoError(t, err)

		// Pre-close pw so that h.close() encounters an error from pw.Close()
		require.NoError(t, pw.Close())

		h := &redirectHandle{pr: pr, pw: pw}
		err = h.close()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "error closing redirect handle")
	})

	t.Run("returns error when pr is already closed", func(t *testing.T) {
		pr, pw, err := os.Pipe()
		require.NoError(t, err)

		// Pre-close pr so that h.close() encounters an error from pr.Close()
		require.NoError(t, pr.Close())

		h := &redirectHandle{pr: pr, pw: pw}
		err = h.close()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "error closing redirect handle")
	})

	t.Run("close with nil pw only closes pr", func(t *testing.T) {
		pr, _, err := os.Pipe()
		require.NoError(t, err)

		h := &redirectHandle{pr: pr, pw: nil}
		require.NoError(t, h.close())
	})

	t.Run("close with nil pr only closes pw", func(t *testing.T) {
		_, pw, err := os.Pipe()
		require.NoError(t, err)

		h := &redirectHandle{pr: nil, pw: pw}
		require.NoError(t, h.close())
	})
}

func TestWriteToStdinAfterProcessExit(t *testing.T) {
	// Regression test: when a process exits naturally, the goroutine in StartProcessCommon
	// closes stdinRedirect. A subsequent WriteToStdin on the same pid must not panic,
	// even if it races with that close. The fix is closeOnce on redirectHandle.
	pm := NewProcessManagerWithFs(afero.NewMemMapFs())

	proc, err := pm.StartProcess(&StartProcess{
		LaunchCommand: LaunchCommand{Command: platformEchoCommand("hello")},
		Stdout:        StreamRedirect{Mode: Stream},
		Stdin:         StdinBuffer,
	})
	require.NoError(t, err)

	// Wait for the process to exit; this unblocks after ProcessExited() is called
	// in the goroutine, which fires before stdinRedirect.close().
	_, err = pm.WaitProcess(context.Background(), proc.Pid)
	require.NoError(t, err)

	// Give the exit goroutine time to reach stdinRedirect.close()
	time.Sleep(50 * time.Millisecond)

	// WriteToStdin may race with or follow the goroutine's close.
	// It must not panic; an error (write to closed pipe) is acceptable.
	_ = pm.WriteToStdin(proc.Pid, []byte("late"))
}

func TestWriteToStdinConcurrentWithProcessExit(t *testing.T) {
	// Primarily a race-detector test (-race): WriteToStdin and the process exit
	// goroutine both call stdinRedirect.close() on the same redirectHandle.
	// Neither should panic or data-race.
	pm := NewProcessManagerWithFs(afero.NewMemMapFs())

	proc, err := pm.StartProcess(&StartProcess{
		LaunchCommand: LaunchCommand{Command: platformEchoCommand("hello")},
		Stdout:        StreamRedirect{Mode: Stream},
		Stdin:         StdinBuffer,
	})
	require.NoError(t, err)

	// Race WriteToStdin against natural process exit
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = pm.WriteToStdin(proc.Pid, []byte("hello"))
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("WriteToStdin did not complete")
	}

	_, _ = pm.WaitProcess(context.Background(), proc.Pid)
}

func TestShutdownNoProcesses(t *testing.T) {
	pm := NewProcessManagerWithFs(afero.NewMemMapFs())
	require.NoError(t, pm.Shutdown(false))
	require.NoError(t, pm.Shutdown(false))
}

func TestShutdownRejectsNewWork(t *testing.T) {
	pm := NewProcessManagerWithFs(afero.NewMemMapFs())
	sp := &StartProcess{
		LaunchCommand: LaunchCommand{Command: platformSleepCommand(1)},
		Stdout:        StreamRedirect{Mode: Stream},
		Stderr:        StreamRedirect{Mode: Stream},
	}

	proc, err := pm.StartProcess(sp)
	require.NoError(t, err)

	require.NoError(t, pm.Shutdown(false))

	_, err = pm.StartProcess(sp)
	require.ErrorIs(t, err, ErrNewProcessInShutdown)

	_, err = pm.ExecCommand(&LaunchCommand{Command: platformEchoCommand("hello")})
	require.ErrorIs(t, err, ErrNewProcessInShutdown)

	_, _ = pm.WaitProcess(context.Background(), proc.Pid)
}

// Helper utilities

func platformEchoCommand(text string) []string {
	if runtime.GOOS == "windows" {
		return []string{"powershell", "-Command", fmt.Sprintf("Write-Output %s", quotePowerShell(text))}
	}
	return []string{"echo", text}
}

func platformSleepCommand(seconds int) []string {
	if runtime.GOOS == "windows" {
		return []string{"powershell", "-Command", fmt.Sprintf("Start-Sleep %d", seconds)}
	}
	return []string{"sleep", fmt.Sprintf("%d", seconds)}
}

func platformReadStdinCommand() []string {
	if runtime.GOOS == "windows" {
		return []string{"powershell", "-Command", "$in=[Console]::In.ReadToEnd(); Write-Output $in"}
	}
	return []string{"cat"}
}

func quotePowerShell(value string) string {
	if strings.ContainsAny(value, " \t'\"") {
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	}
	return value
}
