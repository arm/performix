// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package process

import "os/exec"

func startWithAffinity(cmd *exec.Cmd, _ []string) error { return cmd.Start() }
