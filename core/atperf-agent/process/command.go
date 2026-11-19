// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
)

// launchCommand keeps the command and its platform-specific start behavior together.
type launchCommand struct {
	command            *exec.Cmd
	affinity           []string
	lifetime           commandLifetime
	cancelled          atomic.Bool
	temporaryDirectory *TemporaryDirectory
	temporaryPath      string
	cleanupErr         error
}

func (l *launchCommand) start() error {
	if err := l.prepareTemporaryDirectory(); err != nil {
		return err
	}
	l.lifetime.configureCancellation(l.command)
	if l.command.Cancel != nil {
		cancel := l.command.Cancel
		l.command.Cancel = func() error {
			err := cancel()
			if err == nil {
				l.cancelled.Store(true)
			}
			return err
		}
	}
	if err := l.lifetime.start(l.command, l.affinity); err != nil {
		return errors.Join(err, l.removeTemporaryDirectory())
	}
	return nil
}

// CleanupError reports failed resource cleanup after the program has exited.
// Its presence distinguishes a completed command from a failed wait or launch.
type CleanupError struct{ Err error }

func (e *CleanupError) Error() string { return "process cleanup failed: " + e.Err.Error() }
func (e *CleanupError) Unwrap() error { return e.Err }

func (l *launchCommand) wait() error {
	err := l.command.Wait()
	l.lifetime.close()
	if err := l.removeTemporaryDirectory(); err != nil {
		l.cleanupErr = &CleanupError{Err: err}
	}
	return err
}

func (l *launchCommand) kill() error { return l.lifetime.kill(l.command.Process) }

type LaunchCommand struct {
	// Command describes a direct process launch. The first entry is the
	// executable and every remaining entry is passed to it as a separate
	// argument. Callers that intentionally need a script must name the shell or
	// interpreter explicitly, for example: []string{"sh", "-c", source}.
	// Bare executable names use the inherited PATH unless Environment overrides
	// it. Explicit executable paths are unaffected by PATH overrides.
	Command            []string
	AsPrivileged       bool
	Affinity           []string
	Environment        map[string]string
	WorkingDirectory   string
	TemporaryDirectory *TemporaryDirectory
}

// newLaunchCommand resolves bare names using a request's PATH override without
// changing the agent's environment (other requests can launch concurrently).
func newLaunchCommand(request *LaunchCommand) *launchCommand {
	parts, overrides := request.Command, request.Environment
	var environment []string
	if len(overrides) > 0 {
		environment = mergeEnv(overrides)
	}
	lookupOverride := false
	for name := range overrides {
		if name == "PATH" {
			lookupOverride = true
			break
		}
		if runtime.GOOS != "windows" {
			continue
		}
		if strings.EqualFold(name, "PATH") || strings.EqualFold(name, "PATHEXT") {
			lookupOverride = true
			break
		}
	}
	executable := parts[0]
	var lookupErr error
	if lookupOverride && filepath.Base(executable) == executable && filepath.VolumeName(executable) == "" {
		executable, lookupErr = lookPathInEnvironment(executable, environment)
		if lookupErr != nil {
			executable = parts[0]
		}
	}
	//nolint:gosec // This API intentionally launches caller-supplied executables.
	cmd := exec.Command(executable, parts[1:]...)
	cmd.Args[0] = parts[0]
	cmd.Env = environment
	cmd.Dir = request.WorkingDirectory
	if lookupErr != nil {
		cmd.Err = lookupErr
	}
	return &launchCommand{command: cmd, affinity: request.Affinity, temporaryDirectory: request.TemporaryDirectory}
}

func lookPathInEnvironment(name string, environment []string) (string, error) {
	getenv := func(key string) string {
		for _, entry := range environment {
			k, v, _ := strings.Cut(entry, "=")
			if k == key || (runtime.GOOS == "windows" && strings.EqualFold(k, key)) {
				return v
			}
		}
		return ""
	}
	extensions := []string{""}
	if runtime.GOOS == "windows" {
		extensions = nil
		if filepath.Ext(name) != "" {
			extensions = append(extensions, "")
		}
		pathExt := getenv("PATHEXT")
		if pathExt == "" {
			pathExt = ".com;.exe;.bat;.cmd"
		}
		for _, ext := range strings.Split(pathExt, ";") {
			if ext != "" {
				if !strings.HasPrefix(ext, ".") {
					ext = "." + ext
				}
				extensions = append(extensions, ext)
			}
		}
	}
	var permissionErr error
	for _, directory := range filepath.SplitList(getenv("PATH")) {
		if directory == "" && runtime.GOOS == "windows" {
			continue
		}
		for _, extension := range extensions {
			candidate := filepath.Join(directory, name+extension)
			absolute, err := filepath.Abs(candidate)
			if err != nil {
				return "", err
			}
			if runtime.GOOS == "windows" {
				info, err := os.Stat(absolute)
				if permissionErr == nil && errors.Is(err, os.ErrPermission) {
					permissionErr = err
				}
				if err != nil || info.IsDir() {
					continue
				}
			} else if _, err := exec.LookPath(absolute); err != nil {
				if permissionErr == nil && errors.Is(err, os.ErrPermission) {
					permissionErr = err
				}
				continue
			}
			// Preserve Go's protection against implicitly executing a relative PATH entry.
			if !filepath.IsAbs(candidate) {
				return "", &exec.Error{Name: name, Err: exec.ErrDot}
			}
			return absolute, nil
		}
	}
	if permissionErr != nil {
		return "", &exec.Error{Name: name, Err: permissionErr}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

var (
	ErrMissingLaunchCommand = errors.New("missing launch command")
	ErrMissingExecutable    = errors.New("missing executable")
)

// launchCommandValidationError reports which request field is invalid without
// including the field's value, which may contain credentials or other private
// data. It is private so callers cannot depend on validation presentation
// details.
type launchCommandValidationError struct {
	field  string
	reason string
}

func (e *launchCommandValidationError) Error() string {
	return fmt.Sprintf("invalid %s: %s", e.field, e.reason)
}

// ValidateLaunchCommand checks the shared direct-process contract. Valid input
// is never trimmed, split, quoted, normalized or otherwise rewritten.
func ValidateLaunchCommand(command *LaunchCommand) error {
	if command == nil {
		return ErrMissingLaunchCommand
	}
	if len(command.Command) == 0 || command.Command[0] == "" {
		return ErrMissingExecutable
	}

	for index, value := range command.Command {
		if strings.ContainsRune(value, '\x00') {
			field := "argument"
			if index == 0 {
				field = "executable"
			}
			return &launchCommandValidationError{field: field, reason: "contains a NUL character"}
		}
	}

	for name, value := range command.Environment {
		switch {
		case name == "":
			return &launchCommandValidationError{field: "environment name", reason: "is empty"}
		case strings.ContainsRune(name, '='):
			return &launchCommandValidationError{field: "environment name", reason: "contains '='"}
		case strings.ContainsRune(name, '\x00'):
			return &launchCommandValidationError{field: "environment name", reason: "contains a NUL character"}
		case strings.ContainsRune(value, '\x00'):
			return &launchCommandValidationError{field: "environment value", reason: "contains a NUL character"}
		}
	}

	if directory := command.TemporaryDirectory; directory != nil {
		if directory.EnvironmentVariable == "" || strings.ContainsAny(directory.EnvironmentVariable, "=\x00") {
			return &launchCommandValidationError{field: "temporary directory environment name", reason: "is invalid"}
		}
		if strings.ContainsRune(directory.ParentDirectory, '\x00') {
			return &launchCommandValidationError{field: "temporary directory parent", reason: "contains a NUL character"}
		}
	}

	if strings.ContainsRune(command.WorkingDirectory, '\x00') {
		return &launchCommandValidationError{field: "working directory", reason: "contains a NUL character"}
	}

	return nil
}
