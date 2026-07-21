// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
)

var LogsCmd = NewLogsCmd()

func NewLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs <run_id>",
		Short: "Print the log file for a run.",
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupRunSub,
		},
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return printLogs(args[0], cmd.OutOrStdout())
		},
	}

	return cmd
}

func printLogs(runId string, out io.Writer) error {
	jsonOutput := viper.GetBool("json")

	dataDir, err := userdirs.DataDir()
	if err != nil {
		return err
	}

	path := filepath.Join(dataDir, "runs", runId, "log.json")
	f, err := os.Open(path)
	if err != nil {
		return message.New(message.CliCmdRunLogsLogFileOpenFailed).WithMetadata(map[string]string{"runId": runId, "filepath": path}).WithCause(err)
	}
	defer f.Close()

	if jsonOutput {
		_, err = io.Copy(out, f)
	} else {
		err = printFormattedLog(out, f)
	}

	if err != nil {
		return message.New(message.CliCmdRunLogsLogFilePrintError).WithMetadata(map[string]string{"runId": runId, "filepath": path}).WithCause(err)
	}

	return nil
}

func printFormattedLog(out io.Writer, file *os.File) error {
	type logLine struct {
		Message   string          `json:"message"`
		Severity  string          `json:"severity"`
		Timestamp string          `json:"timestamp"`
		Context   json.RawMessage `json:"context"`
	}

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 4*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry logLine
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			fmt.Fprintln(out, line)
			continue
		}

		timestamp := entry.Timestamp
		if timestamp == "" {
			timestamp = "UNKNOWN"
		}

		severity := strings.ToUpper(entry.Severity)
		if severity == "" {
			severity = "UNKNOWN"
		}

		message := entry.Message
		if message == "" {
			message = "(no message)"
		}

		fmt.Fprintf(out, "[%s][%s] %s", timestamp, severity, message)

		if len(entry.Context) > 0 && string(entry.Context) != "null" {
			fmt.Fprintf(out, " %s", entry.Context)
		}

		fmt.Fprintln(out)
	}

	return scanner.Err()
}
