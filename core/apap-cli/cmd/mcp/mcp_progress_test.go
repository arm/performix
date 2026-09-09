// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file tests progress output for MCP client commands.
package mcp

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func TestConfigureMCPProgressWriter(t *testing.T) {
	writer := progress.NewWriter()
	configureMCPProgressWriter(writer, io.Discard, mcpProgressConfig{
		autoStop:    true,
		doneString:  "complete",
		errorString: "failed",
		separator:   " - ",
	})

	style := writer.Style()
	assert.Equal(t, progress.StyleBlocks.Name, style.Name)
	assert.Equal(t, "complete", style.Options.DoneString)
	assert.Equal(t, "failed", style.Options.ErrorString)
	assert.Equal(t, " - ", style.Options.Separator)
	assert.True(t, style.Options.KeepTrackersTogether)
	assert.False(t, style.Visibility.ETA)
	assert.False(t, style.Visibility.Percentage)
	assert.False(t, style.Visibility.Time)
	assert.False(t, style.Visibility.Value)
}

func TestMCPStatusShowsProgressWhileListingClients(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	service := &registrationTestService{
		listing:     registrationTestListing(),
		listStarted: started,
		listRelease: release,
	}
	command := newMCPStatusCmd(registrationTestDeps(service))
	command.SetOut(io.Discard)
	var progressOutput bytes.Buffer
	command.SetErr(&progressOutput)
	done := make(chan error, 1)

	go func() {
		_, err := command.ExecuteC()
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		require.FailNow(t, "MCP client listing did not start")
	}
	time.Sleep(2 * mcpProgressUpdateFrequency)
	close(release)
	require.NoError(t, <-done)

	plainProgress := text.StripEscape(progressOutput.String())
	assert.Contains(t, plainProgress, "Detecting MCP clients...")
	assert.Contains(t, progressOutput.String(), "Detected MCP clients:")
	assert.True(t, strings.HasSuffix(progressOutput.String(), "Detected MCP clients:\n"))
}

func TestMCPOperationShowsProgressWhileListingClients(t *testing.T) {
	for _, test := range []struct {
		name        string
		messageCode message.MessageCode
		expected    string
	}{
		{name: "install", messageCode: message.CliCmdMcpInstallDetectionProgress, expected: "Detecting available MCP clients..."},
		{name: "uninstall", messageCode: message.CliCmdMcpUninstallDetectionProgress, expected: "Detecting configured MCP clients..."},
	} {
		t.Run(test.name, func(t *testing.T) {
			started := make(chan struct{}, 1)
			release := make(chan struct{})
			service := &registrationTestService{
				listing:     registrationTestListing(),
				listStarted: started,
				listRelease: release,
			}
			command := &cobra.Command{}
			var progressOutput bytes.Buffer
			command.SetErr(&progressOutput)
			done := make(chan error, 1)

			go func() {
				_, _, shutdown, err := connectAndListWithProgress(
					command,
					registrationTestDeps(service),
					"",
					test.messageCode,
					false,
				)
				if shutdown != nil {
					err = errors.Join(err, shutdown())
				}
				done <- err
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				require.FailNow(t, "MCP client listing did not start")
			}
			time.Sleep(2 * mcpProgressUpdateFrequency)
			close(release)
			require.NoError(t, <-done)

			assert.Contains(t, progressOutput.String(), test.expected)
			assert.NotContains(t, progressOutput.String(), "Detected MCP clients:")
			assert.True(
				t,
				strings.HasSuffix(
					progressOutput.String(),
					text.CursorUp.Sprint()+text.EraseLine.Sprint(),
				),
			)
		})
	}
}

func TestMCPInstallUsesProgressAsHumanOutput(t *testing.T) {
	service := &registrationTestService{listing: registrationTestListing()}
	command := newMCPInstallCmd(registrationTestDeps(service))
	command.SilenceUsage = true
	var stdout bytes.Buffer
	var progressOutput bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&progressOutput)
	command.SetArgs([]string{"cursor"})

	_, err := command.ExecuteC()
	require.NoError(t, err)
	assert.Empty(t, stdout.String())
	plainProgressOutput := text.StripEscape(progressOutput.String())
	assert.Contains(t, plainProgressOutput, "Adding Arm Performix MCP to cursor - done")
	assert.NotContains(t, plainProgressOutput, "done!")
}

func TestMCPProgressUsesIndeterminateBlockStyleWithoutTiming(t *testing.T) {
	command := &cobra.Command{}
	var output bytes.Buffer
	command.SetErr(&output)

	writer, trackers, done, err := startMCPProgress(
		command,
		[]string{"claude-code", "codex"},
		message.CliCmdMcpInstallProgress,
	)
	require.NoError(t, err)
	require.Len(t, trackers, 2)
	assert.Equal(t, "StyleBlocks", writer.Style().Name)
	assert.True(t, trackers[0].IsIndeterminate())
	assert.True(t, trackers[1].IsIndeterminate())
	assert.Equal(t, uint64(0), trackers[0].Index)
	assert.Equal(t, uint64(1), trackers[1].Index)
	assert.True(t, writer.Style().Options.KeepTrackersTogether)
	assert.False(t, writer.Style().Visibility.Percentage)
	assert.False(t, writer.Style().Visibility.Time)
	assert.False(t, writer.Style().Visibility.ETA)
	assert.False(t, writer.Style().Visibility.ETAOverall)
	assert.False(t, writer.Style().Visibility.Value)
	assert.Equal(t, text.FgGreen.Sprint("done"), writer.Style().Options.DoneString)
	assert.Equal(t, text.FgRed.Sprint("failed"), writer.Style().Options.ErrorString)

	trackers[0].MarkAsDone()
	trackers[1].MarkAsErrored()
	stopMCPProgress(writer, done)
	plainOutput := text.StripEscape(output.String())
	assert.Contains(t, plainOutput, "Adding Arm Performix MCP to claude-code")
	assert.Contains(t, plainOutput, "Adding Arm Performix MCP to codex")
	assert.Less(t,
		strings.Index(plainOutput, "Adding Arm Performix MCP to claude-code"),
		strings.Index(plainOutput, "Adding Arm Performix MCP to codex"),
	)
	assert.Contains(t, plainOutput, " - done")
	assert.Contains(t, plainOutput, " - failed")
	assert.NotContains(t, plainOutput, "done!")
	assert.NotContains(t, plainOutput, "ETA")
	assert.NotContains(t, plainOutput, "%")
}
