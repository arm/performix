// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

func writeRunLog(t *testing.T, runID, content string) string {
	t.Helper()

	dataDir := t.TempDir()
	runDir := filepath.Join(dataDir, "runs", runID)
	require.NoError(t, os.MkdirAll(runDir, perms.LocalDirPerm))

	logPath := filepath.Join(runDir, "log.json")
	require.NoError(t, os.WriteFile(logPath, []byte(content), perms.LocalFilePerm))

	return dataDir
}

func TestLogsCmdPrintsFormattedTextByDefault(t *testing.T) {
	const runID = "abc123"
	logContent := `{"message":"Starting run","severity":"info","timestamp":"2026-02-04T10:31:40Z"}
{"context":{"stage":"Acquiring target lock"},"message":"Stage started","severity":"info","timestamp":"2026-02-04T10:31:40Z"}
`
	dataDir := writeRunLog(t, runID, logContent)
	t.Setenv(util.ApplyEnvPrefix("DATA_DIR"), dataDir)

	cmd := NewLogsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{runID})

	require.NoError(t, cmd.Execute())

	expected := "" +
		"[2026-02-04T10:31:40Z][INFO] Starting run\n" +
		"[2026-02-04T10:31:40Z][INFO] Stage started {\"stage\":\"Acquiring target lock\"}\n"
	require.Equal(t, expected, buf.String())
}

func TestLogsCmdRespectsJsonFlag(t *testing.T) {
	const runID = "run-json"
	logContent := `{"message":"Hello","severity":"info","timestamp":"2026-02-04T10:31:40Z"}
`
	dataDir := writeRunLog(t, runID, logContent)
	t.Setenv(util.ApplyEnvPrefix("DATA_DIR"), dataDir)

	t.Cleanup(func() {
		viper.Set("json", false)
	})
	viper.Set("json", true)

	cmd := NewLogsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{runID})

	require.NoError(t, cmd.Execute())
	require.Equal(t, logContent, buf.String())
}

func TestLogsCmdReturnsErrorWhenFileIsMissing(t *testing.T) {
	t.Setenv(util.ApplyEnvPrefix("DATA_DIR"), t.TempDir())
	const runID = "missing-run"

	cmd := NewLogsCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{runID})

	err := cmd.Execute()
	require.Error(t, err)

	var msgErr message.Message
	require.True(t, errors.As(err, &msgErr))
	require.Equal(t, message.CliCmdRunLogsLogFileOpenFailed, msgErr.Code())
	require.Equal(t, runID, msgErr.Metadata()["runId"])
	_, exists := msgErr.Metadata()["filepath"]
	require.True(t, exists, "filepath metadata should be present")
}

func TestLogsCmdReturnsErrorWhenFormattingFails(t *testing.T) {
	const runID = "big-run"
	huge := strings.Repeat("A", 1024*1024)
	logContent := `{"message":"` + huge + `","severity":"info","timestamp":"2026-02-04T10:31:40Z"}
`
	dataDir := writeRunLog(t, runID, logContent)
	t.Setenv(util.ApplyEnvPrefix("DATA_DIR"), dataDir)

	cmd := NewLogsCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{runID})

	err := cmd.Execute()
	require.Error(t, err)

	var msgErr message.Message
	require.True(t, errors.As(err, &msgErr))
	require.Equal(t, message.CliCmdRunLogsLogFilePrintError, msgErr.Code())
	require.Equal(t, runID, msgErr.Metadata()["runId"])
	require.Contains(t, msgErr.Metadata()["filepath"], runID)
}

func TestLogsCmdPrintsFallbacksForMissingFields(t *testing.T) {
	const runID = "fallbacks"
	logContent := `{"message":"","severity":"","timestamp":"","context":null}
`
	dataDir := writeRunLog(t, runID, logContent)
	t.Setenv(util.ApplyEnvPrefix("DATA_DIR"), dataDir)

	cmd := NewLogsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{runID})

	require.NoError(t, cmd.Execute())
	require.Equal(t, "[UNKNOWN][UNKNOWN] (no message)\n", buf.String())
}

func TestLogsCmdHandlesMalformedJson(t *testing.T) {
	const runID = "not-json"
	logContent := "\nnot-json\n" + `{"message":"ok","severity":"info","timestamp":"2026-02-04T10:31:40Z"}` + "\n"
	dataDir := writeRunLog(t, runID, logContent)
	t.Setenv(util.ApplyEnvPrefix("DATA_DIR"), dataDir)

	cmd := NewLogsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{runID})

	require.NoError(t, cmd.Execute())
	require.Equal(t, "not-json\n[2026-02-04T10:31:40Z][INFO] ok\n", buf.String())
}
