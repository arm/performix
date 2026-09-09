// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file provides the shared indeterminate progress presentation used while
// the engine discovers or modifies MCP clients.
package mcp

import (
	"io"
	"time"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

// A 40 ms interval limits terminal redraws to 25 frames per second, which
// keeps the indeterminate indicator smooth without redrawing unnecessarily.
const mcpProgressUpdateFrequency = 40 * time.Millisecond

type mcpProgressConfig struct {
	autoStop    bool
	doneString  string
	errorString string
	separator   string
}

// startMCPProgress creates one indeterminate tracker per client. It follows the
// block style used by recipe progress, but omits timing and percentage fields
// because MCP client management does not report measurable intermediate work.
func startMCPProgress(
	cmd *cobra.Command,
	ids []string,
	progressMessageCode message.MessageCode,
) (progress.Writer, []*progress.Tracker, <-chan struct{}, error) {
	return startMCPProgressWriter(cmd, ids, progressMessageCode, mcpProgressConfig{
		doneString:  text.FgGreen.Sprint("done"),
		errorString: text.FgRed.Sprint("failed"),
		separator:   " - ",
	})
}

func startMCPStatusProgress(
	cmd *cobra.Command,
	progressMessageCode message.MessageCode,
) (progress.Writer, []*progress.Tracker, <-chan struct{}, error) {
	return startMCPProgressWriter(cmd, []string{""}, progressMessageCode, mcpProgressConfig{
		autoStop:    true,
		errorString: text.FgRed.Sprint(" failed"),
	})
}

func startMCPProgressWriter(
	cmd *cobra.Command,
	ids []string,
	progressMessageCode message.MessageCode,
	config mcpProgressConfig,
) (progress.Writer, []*progress.Tracker, <-chan struct{}, error) {
	if viper.GetBool("json") {
		return nil, nil, nil, nil
	}

	progressWriter := progress.NewWriter()
	configureMCPProgressWriter(progressWriter, cmd.ErrOrStderr(), config)

	trackers := make([]*progress.Tracker, 0, len(ids))
	for index, id := range ids {
		catalogMessage, err := message.LookupMessage(
			message.New(progressMessageCode).WithMetadata(map[string]string{"client": id}),
		)
		if err != nil {
			return nil, nil, nil, err
		}
		tracker := &progress.Tracker{
			Index:   uint64(index), // #nosec G115 - range indices are non-negative.
			Message: catalogMessage.Message,
		}
		trackers = append(trackers, tracker)
		progressWriter.AppendTracker(tracker)
	}

	done := make(chan struct{})
	go func() {
		progressWriter.Render()
		close(done)
	}()
	// Ensure Stop cannot run before Render has installed its cancellation
	// function. Client management commands can otherwise finish within a single
	// scheduler timeslice and leave the renderer running indefinitely.
	for !progressWriter.IsRenderInProgress() {
		time.Sleep(time.Millisecond)
	}
	return progressWriter, trackers, done, nil
}

// configureMCPProgressWriter applies the common appearance and rendering
// settings used by status, install, and uninstall progress indicators.
func configureMCPProgressWriter(
	progressWriter progress.Writer,
	output io.Writer,
	config mcpProgressConfig,
) {
	progressWriter.SetAutoStop(config.autoStop)
	progressWriter.SetOutputWriter(output)
	progressWriter.SetTrackerLength(30)
	progressWriter.SetUpdateFrequency(mcpProgressUpdateFrequency)
	progressWriter.SetStyle(progress.StyleBlocks)
	// StyleBlocks advances its indicator on a separate 125 ms timer. A zero
	// duration uses go-pretty's documented per-render animation mode, leaving the
	// writer update frequency as the single control for animation speed.
	progressWriter.Style().Chars.Indeterminate =
		progress.IndeterminateIndicatorMovingBackAndForth("▒█▒", 0)
	progressWriter.SetSortBy(progress.SortByIndex)
	progressWriter.SetTrackerPosition(progress.PositionRight)
	progressWriter.Style().Visibility.ETA = false
	progressWriter.Style().Visibility.ETAOverall = false
	progressWriter.Style().Visibility.Percentage = false
	progressWriter.Style().Visibility.Speed = false
	progressWriter.Style().Visibility.SpeedOverall = false
	progressWriter.Style().Visibility.Time = false
	progressWriter.Style().Visibility.TrackerOverall = false
	progressWriter.Style().Visibility.Value = false
	progressWriter.Style().Options.Separator = config.separator
	// The progress style's completion colours also colour the client message.
	// Colour terminal labels directly so the rest of each row stays plain.
	progressWriter.Style().Options.DoneString = config.doneString
	progressWriter.Style().Options.ErrorString = config.errorString
	progressWriter.Style().Options.KeepTrackersTogether = true
}

func markMCPProgressComplete(trackers []*progress.Tracker, index int, failed bool) {
	if trackers == nil {
		return
	}
	if failed {
		trackers[index].MarkAsErrored()
		return
	}
	trackers[index].MarkAsDone()
}

func stopMCPProgress(progressWriter progress.Writer, done <-chan struct{}) {
	if progressWriter == nil {
		return
	}
	progressWriter.Stop()
	<-done
}
