// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package shellresolution

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
)

// ShellifyWorkload takes a launch workload, attempts to find a usable shell on the target using the provided CommandRunner,
// and returns updated copies of the `Command` and `RawCommand` fields, interpreted through a shell. For example:
//
//	Workload:
//	  {Command: []string{"myCommand", "-a", "some string"}, RawCommand: `myCommand -a "some string"`}
//	Available shell is determined to be:
//	  /bin/bash
//	Returned values:
//	  (`/bin/bash -c "myCommand -a \"some string\""`, []string{"/bin/bash", "-c", "myCommand -a \"some string\""}, nil)
//
// Returns a Message if no shell is found (or on other error cases)
func ShellifyWorkload(platformOS conductor.OS, cmdRunner conductor.CommandRunner, workload *tool.WorkloadLaunch) (string, []string, message.Message) {
	switch platformOS {
	case conductor.Android, conductor.Linux, conductor.Darwin:
		return shellifyWorkloadUnix(cmdRunner, workload)
	case conductor.Win:
		return shellifyWorkloadWindows(cmdRunner, workload)
	default:
		return "", nil, message.New(message.EngineCommonUnsupportedTargetOs).WithMetadata(map[string]string{"os": string(platformOS)})
	}
}

func shellifyWorkloadUnix(cmdRunner conductor.CommandRunner, workload *tool.WorkloadLaunch) (string, []string, message.Message) {
	rawCmd := workload.RawCommand

	fallback := "/bin/sh"
	// Attempt to use $SHELL
	shell := getShellEnvUnix(cmdRunner)
	if shell == "" {
		shell = fallback
		log.Debugf("shellifyWorkloadUnix: $SHELL not defined, falling back to %v", shell)
	}

	if tryShellPathUnix(cmdRunner, shell) {
		updatedRawCmd := fmt.Sprintf("%s -c %s", quoteIfNeeded(shell), strconv.Quote(rawCmd))
		updatedCmdSlice := []string{shell, "-c", rawCmd}
		return updatedRawCmd, updatedCmdSlice, nil
	}

	metadata := map[string]string{
		"envVar":   "$SHELL",
		"fallback": fallback,
	}
	return "", []string{}, message.New(message.EngineConductorShellresolutionNoShellFound).WithMetadata(metadata)
}

// tryShellPathUnix returns true if there exists a working shell at the provided path
func tryShellPathUnix(cmdRunner conductor.CommandRunner, shellPath string) bool {
	_, _, err := cmdRunner.RunCommand(fmt.Sprintf("%v -c true", quoteIfNeeded(shellPath)))
	return err == nil
}

// getShellEnvUnix returns the value of the $SHELL env var, or "" if the value isn't defined or couldn't be accessed
func getShellEnvUnix(cmdRunner conductor.CommandRunner) string {
	out, _, err := cmdRunner.RunCommand("echo $SHELL")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func shellifyWorkloadWindows(cmdRunner conductor.CommandRunner, workload *tool.WorkloadLaunch) (string, []string, message.Message) {
	rawCmd := workload.RawCommand

	fallback := "cmd.exe"
	// Attempt to use $SHELL
	shell := getShellEnvWindows(cmdRunner)
	if shell == "" {
		shell = fallback
		log.Debugf("shellifyWorkloadWindows: %%SHELL%% not defined, falling back to %v", shell)
	}

	args := getShellFlagsWindows(shell)

	if tryShellPathWindows(cmdRunner, shell, strings.Join(args, " ")) {
		updatedRawCmd := fmt.Sprintf("%s %s %s", quoteIfNeeded(shell), strings.Join(args, " "), strconv.Quote(rawCmd))
		updatedCmdSlice := []string{shell}
		updatedCmdSlice = append(updatedCmdSlice, args...)
		updatedCmdSlice = append(updatedCmdSlice, rawCmd)
		return updatedRawCmd, updatedCmdSlice, nil
	}

	metadata := map[string]string{
		"envVar":   "%SHELL%",
		"fallback": fallback,
	}
	return "", []string{}, message.New(message.EngineConductorShellresolutionNoShellFound).WithMetadata(metadata)
}

// tryShellPathWindows returns true if there exists a working shell at the provided path
func tryShellPathWindows(cmdRunner conductor.CommandRunner, shellPath string, args string) bool {
	_, _, err := cmdRunner.RunCommand(fmt.Sprintf("%v %v exit", quoteIfNeeded(shellPath), args))
	return err == nil
}

// getShellEnvWindows returns the value of the %SHELL% env var, or "" if the value isn't defined or couldn't be accessed
// %SHELL% is set by the OpenSSH server on the target when connecting
func getShellEnvWindows(cmdRunner conductor.CommandRunner) string {
	out, _, err := cmdRunner.RunCommand("echo %SHELL%")
	if err != nil {
		return ""
	}
	trimmedOut := strings.TrimSpace(out)
	if trimmedOut == "%SHELL%" {
		return ""
	}
	return trimmedOut
}

// getShellFlagsWindows returns the flags to provide to the specified shell
func getShellFlagsWindows(shellPath string) []string {
	lowerPath := strings.ToLower(shellPath)
	if strings.HasSuffix(lowerPath, "powershell.exe") || strings.HasSuffix(lowerPath, "powershell") {
		return []string{"-NoLogo", "-WindowStyle", "Hidden", "-Command"}
	}

	if strings.HasSuffix(lowerPath, "cmd.exe") || strings.HasSuffix(lowerPath, "cmd") {
		return []string{"/c"}
	}

	return []string{"-c"}
}

func quoteIfNeeded(path string) string {
	whitespaceRegex := regexp.MustCompile(`^.*\s.*$`)
	matches := whitespaceRegex.FindStringSubmatch(path)
	// Return plain string if it doesn't contain any whitespace
	if len(matches) == 0 {
		return path
	}
	return strconv.Quote(path)
}
