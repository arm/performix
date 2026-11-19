// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"time"
)

func runProcessTree() error {
	if os.Getenv("APX_PROCESS_PROBE_TREE") == "child" {
		time.Sleep(time.Minute)
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	child := exec.Command(executable) //nolint:gosec // Fixed probe executable, no caller-supplied command.
	child.Env = append(os.Environ(), "APX_PROCESS_PROBE_TREE=child")
	if os.Getenv("APX_PROCESS_PROBE_TREE_OUTPUT") != "discard" {
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
	}
	if err := child.Start(); err != nil {
		return err
	}
	data, err := json.Marshal([]int{os.Getpid(), child.Process.Pid})
	if err != nil {
		return err
	}
	if err := os.WriteFile(os.Getenv("APX_PROCESS_PROBE_TREE_PIDS"), data, 0600); err != nil {
		return err
	}
	if os.Getenv("APX_PROCESS_PROBE_TREE") == "exit-parent" {
		for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); {
			if _, err := os.Stat(os.Getenv("APX_PROCESS_PROBE_TREE_RELEASE")); err == nil {
				return nil
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	return child.Wait()
}
