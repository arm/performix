// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package cmd

import (
	"fmt"
	"os"

	log "github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

func setupControllerLogPipe(fd int) error {
	// The workload must not inherit the controller's log pipe and keep it open.
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
		return fmt.Errorf("configure controller log descriptor: %w", err)
	}
	file := os.NewFile(uintptr(fd), "controller-log")
	if err := setupLogging(true, file); err != nil {
		_ = file.Close()
		return err
	}
	log.SetFormatter(&log.JSONFormatter{})
	return nil
}
