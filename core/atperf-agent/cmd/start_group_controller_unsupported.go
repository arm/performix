// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build darwin || windows

package cmd

import (
	"runtime"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func startGroupController(_ []string, _ bool, _ int) error {
	return message.New(message.AgentLifecycleGroupControllerUnsupportedPlatform).
		WithMetadata(map[string]string{
			"os":   runtime.GOOS,
			"arch": runtime.GOARCH,
		})
}
