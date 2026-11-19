// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"strings"
	"unicode/utf16"

	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

const scriptDataVariable = "APX_EXECUTION_SCRIPT_DATA"
const scriptDirectoryVariable = "APX_EXECUTION_SCRIPT_DIRECTORY"

// Only this fixed code appears in process arguments. Script source and values
// travel as an environment payload; the wrapper removes it before invocation.
//
//go:embed execution_windows.ps1
var windowsScriptWrapper string

// PowerShell -EncodedCommand requires base64 of UTF-16LE bytes. Only the fixed
// wrapper uses this encoding; the serialized request below remains UTF-8.
func encodePowerShell(source string) string {
	words := utf16.Encode([]rune(source))
	data := make([]byte, 2*len(words))
	for i, word := range words {
		binary.LittleEndian.PutUint16(data[2*i:], word)
	}
	return base64.StdEncoding.EncodeToString(data)
}

func windowsScriptCommand(request *ScriptRequest, command *process.LaunchCommand) error {
	if request.Shell == ShellCmd && len(request.Arguments) != 0 {
		return NewExecutionError(ExecutionUnsupported)
	}
	for name := range request.Environment {
		if strings.EqualFold(name, scriptDataVariable) || strings.EqualFold(name, scriptDirectoryVariable) {
			return NewExecutionError(ExecutionInvalid)
		}
	}
	// Validate the original values before encoding, otherwise JSON would hide NULs.
	validation := *command
	validation.Command = append([]string{"powershell.exe"}, request.Arguments...)
	if err := ValidateProcessRequest(&validation, Win); err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		Shell       Shell
		Source      string
		Arguments   []string
		Environment map[string]string
	}{
		request.Shell, request.Source, append([]string{}, request.Arguments...), request.Environment,
	})
	if err != nil {
		return NewExecutionError(ExecutionInvalid)
	}
	// Resolve and start the wrapper with the inherited environment. It applies
	// caller overrides after startup, so a script's PATH cannot hide its shell.
	command.Environment = map[string]string{scriptDataVariable: base64.StdEncoding.EncodeToString(payload)}
	if request.Shell == ShellCmd {
		command.TemporaryDirectory = &process.TemporaryDirectory{EnvironmentVariable: scriptDirectoryVariable}
	}
	command.Command = []string{"powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodePowerShell(windowsScriptWrapper)}
	return nil
}
