// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCmdLogsSafeLaunchMetadata(t *testing.T) {
	logger := logrus.StandardLogger()
	output, formatter, level := logger.Out, logger.Formatter, logger.GetLevel()
	t.Cleanup(func() {
		logger.SetOutput(output)
		logger.SetFormatter(formatter)
		logger.SetLevel(level)
	})
	var logs bytes.Buffer
	logger.SetOutput(&logs)
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetLevel(logrus.InfoLevel)

	executable, err := os.Executable()
	require.NoError(t, err)
	_, err = NewProcessManager().buildCmd(&LaunchCommand{
		Command:          []string{executable, "private argument"},
		Environment:      map[string]string{"TOKEN": "private environment value"},
		WorkingDirectory: "private working directory",
	})
	require.NoError(t, err)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &entry))
	assert.Equal(t, "Launch command", entry["msg"])
	assert.Equal(t, executable, entry["executable"])
	assert.Equal(t, float64(1), entry["argumentCount"])
	assert.Equal(t, float64(1), entry["environmentCount"])
	assert.Equal(t, true, entry["hasWorkingDirectory"])
	assert.NotContains(t, logs.String(), "private argument")
	assert.NotContains(t, logs.String(), "private environment value")
	assert.NotContains(t, logs.String(), "private working directory")
}
