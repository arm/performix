// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package cmd

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestControllerLogDescriptor(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "controller-log")
	require.NoError(t, err)
	defer file.Close()
	fd, err := unix.Dup(int(file.Fd()))
	require.NoError(t, err)
	logger := log.StandardLogger()
	output, formatter := logger.Out, logger.Formatter
	t.Cleanup(func() {
		if sink, ok := logger.Out.(*os.File); ok && sink != output {
			_ = sink.Close()
		}
		logger.SetOutput(output)
		logger.SetFormatter(formatter)
	})
	cmd := NewStartGroupControllerCmd()
	require.NoError(t, cmd.Flags().Set("log-fd", strconv.Itoa(fd)))
	require.NoError(t, cmd.PreRunE(cmd, nil))
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	require.NoError(t, err)
	assert.NotZero(t, flags&unix.FD_CLOEXEC, "workload must not inherit the log descriptor")
	log.WithField("pgid", 123).Warn("signal forwarding failed")
	data, err := os.ReadFile(file.Name())
	require.NoError(t, err)
	var record map[string]any
	require.NoError(t, json.Unmarshal(data, &record))
	assert.Equal(t, "warning", record["level"])
	assert.Equal(t, "signal forwarding failed", record["msg"])
	assert.Equal(t, float64(123), record["pgid"])
}
