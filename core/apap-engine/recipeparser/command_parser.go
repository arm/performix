// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipeparser

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/dop251/goja"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/gojautils"
)

// ParseRunCommand takes the goja arg and returns the appropriate run command type
func ParseRunCommand(cmd goja.Value) (conductor.RunCommandSpecificType, error) {
	var runData struct {
		Type conductor.RunCommandType `json:"type"`
	}

	// Regex to allow all unused. Here we parse into a structure that only contains the "type" field.
	allowUnusedRegex, err := regexp.Compile(`.*`)
	if err != nil {
		return nil, errors.New("invalid regex")
	}
	err = gojautils.ParseObjectFromJSWithRegex(cmd, &runData, []*regexp.Regexp{}, []*regexp.Regexp{allowUnusedRegex})
	if err != nil {
		return nil, err
	}

	var runCommand conductor.RunCommandSpecificType
	switch runData.Type {
	case conductor.TypeExec:
		runCommand = &conductor.ExecCommand{}
	case conductor.TypePython:
		runCommand = &conductor.PythonExecCommand{}
	default:
		return nil, fmt.Errorf("invalid run command type")
	}

	// Regex to allow the "type" field to be unused. The RunCommandSpecificType implementations don't have a "type" field.
	allowUnusedTypeRegex, err := regexp.Compile(`^(type)$`)
	if err != nil {
		return nil, errors.New("invalid regex")
	}

	// Regex to allow the "venv" and "runAsAdmin" fields to be unset. These are optional fields.
	allowUnsetVenvRegex, err := regexp.Compile(`^(venv|runAsAdmin)$`)
	if err != nil {
		return nil, errors.New("invalid regex")
	}

	err = gojautils.ParseObjectFromJSWithRegex(cmd, runCommand, []*regexp.Regexp{allowUnsetVenvRegex}, []*regexp.Regexp{allowUnusedTypeRegex})
	if err != nil {
		return nil, err
	}

	return runCommand, nil
}
