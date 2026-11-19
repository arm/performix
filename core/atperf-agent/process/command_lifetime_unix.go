// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package process

import (
	"os"
	"os/exec"
)

type commandLifetime struct{}

func (*commandLifetime) start(cmd *exec.Cmd, affinity []string) error {
	return startWithAffinity(cmd, affinity)
}

func (*commandLifetime) kill(process *os.Process) error { return process.Kill() }
func (*commandLifetime) close()                         {}

func (*commandLifetime) configureCancellation(*exec.Cmd) {}
