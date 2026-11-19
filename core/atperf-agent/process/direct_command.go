// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const directCommandDrainTimeout = 2 * time.Second

// ErrUnsupportedLaunch indicates an executable or option unsupported by the launch path.
var ErrUnsupportedLaunch = errors.New("unsupported launch request")

// validateResolvedCommand checks the target-side path after PATH/PATHEXT lookup.
func validateResolvedCommand(cmd *exec.Cmd) error {
	// Preserve lookup failures: Path may come from the ambient PATH even when
	// resolution against the request's environment failed. Start reports Err.
	if cmd.Err != nil {
		return nil
	}
	if runtime.GOOS == "windows" {
		ext := strings.ToLower(filepath.Ext(cmd.Path))
		if ext == ".cmd" || ext == ".bat" {
			return ErrUnsupportedLaunch
		}
	}
	return nil
}

// NewDirectCommand builds an unprivileged, context-bound command without a shell.
// TemporaryDirectory is created only by RunDirectCommand.
// It does not own descendants. Use RunDirectCommand for ordinary execution;
// bootstrap callers that transfer ownership must manage cancellation and cleanup.
// It shares PATH lookup and environment merging with the agent. Affinity and
// elevation belong to the existing agent process/privilege APIs, not bootstrap.
func NewDirectCommand(ctx context.Context, request *LaunchCommand) (*exec.Cmd, error) {
	if err := ValidateLaunchCommand(request); err != nil {
		return nil, err
	}
	if request.AsPrivileged || len(request.Affinity) != 0 {
		return nil, ErrUnsupportedLaunch
	}
	if err := CheckWorkingDirectory(request.WorkingDirectory); err != nil {
		return nil, err
	}
	resolved := newLaunchCommand(request).command
	if err := validateResolvedCommand(resolved); err != nil {
		return nil, err
	}
	// CommandContext supplies cancellation without changing the resolved argv.
	cmd := exec.CommandContext(ctx, resolved.Path, request.Command[1:]...) //nolint:gosec // Explicit direct execution API.
	cmd.Args = resolved.Args
	cmd.Env = resolved.Env
	cmd.Err = resolved.Err
	cmd.Dir = request.WorkingDirectory
	cmd.WaitDelay = directCommandDrainTimeout
	configureDirectCancellation(cmd)
	return cmd, nil
}

// RunDirectCommand captures a context-bound process and owns its descendants
// until it finishes. Nonzero exit statuses retain the underlying exec.ExitError
// so the caller can distinguish a child result from a launch failure.
func RunDirectCommand(ctx context.Context, request *LaunchCommand) (*CommandResult, error) {
	cmd, err := NewDirectCommand(ctx, request)
	if err != nil {
		return nil, err
	}
	return runDirectCommand(ctx, &launchCommand{command: cmd, temporaryDirectory: request.TemporaryDirectory})
}

func runDirectCommand(ctx context.Context, launch *launchCommand) (*CommandResult, error) {
	cmd := launch.command
	stdout, err := newCommandOutput()
	if err != nil {
		return nil, err
	}
	defer stdout.close()
	stderr, err := newCommandOutput()
	if err != nil {
		return nil, err
	}
	defer stderr.close()
	// File writers keep os/exec from waiting for output before we can stop
	// descendants. Drain the pipes ourselves after the process group is closed.
	cmd.Stdout, cmd.Stderr = stdout.writer, stderr.writer
	err = launch.start()
	if err == nil {
		err = launch.wait()
	}
	// Attribute cancellation while we still have the wait result and know
	// whether the cancellation callback actually stopped the command.
	if err != nil && launch.cancelled.Load() && directCommandWasKilled(cmd) {
		err = ctx.Err()
	}
	closeDirectCommand(cmd)
	out, outErr := stdout.wait(cmd.WaitDelay)
	errOut, errOutErr := stderr.wait(cmd.WaitDelay)
	if err == nil {
		err = errors.Join(outErr, errOutErr)
	}
	result := &CommandResult{Rc: -1, Stdout: out, Stderr: errOut}
	if cmd.ProcessState != nil {
		// #nosec G115 -- preserve the Windows DWORD exit status in the signed result field.
		result.Rc = int32(cmd.ProcessState.ExitCode())
	}
	if launch.cleanupErr != nil {
		err = errors.Join(err, launch.cleanupErr)
	}
	return result, err
}

// commandOutput drains concurrently so a full output pipe cannot block the child.
type commandOutput struct {
	reader, writer *os.File
	done           chan struct{}
	data           []byte
	err            error
}

func newCommandOutput() (*commandOutput, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	output := &commandOutput{reader: reader, writer: writer, done: make(chan struct{})}
	go func() {
		output.data, output.err = io.ReadAll(reader)
		close(output.done)
	}()
	return output, nil
}

func (o *commandOutput) close() {
	_ = o.writer.Close()
	_ = o.reader.Close()
	<-o.done
}

func (o *commandOutput) wait(delay time.Duration) (string, error) {
	_ = o.writer.Close()
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-o.done:
		return string(o.data), o.err
	case <-timer.C:
		// A descendant that deliberately escaped the group may retain the pipe.
		_ = o.reader.Close()
		<-o.done
		return string(o.data), exec.ErrWaitDelay
	}
}
