// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sync/semaphore"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// RetrieveAgentFilesStage is responsible for retrieving files from the target agent after a recipe execution.
type RetrieveAgentFilesStage struct {
	RecipeCollector *recipe.Collector
}

type agentFileTransfer struct {
	recipe.TransferRequest
	logFile bool // Log files do not produce errors if not found
}

type resolvedAgentFileTransfer struct {
	agentFileTransfer
	agentClient targetagentproto.TargetAgentClient
}

// TransferProgress defines the interface for a progress reporter.
type TransferProgress interface {
	Progress(received int64)
}

// MultiTransferProgress reports progress to multiple TransferProgress instances.
type MultiTransferProgress struct {
	transfers []TransferProgress
}

func (a *MultiTransferProgress) Progress(received int64) {
	for _, t := range a.transfers {
		t.Progress(received)
	}
}

type FileRetrievalObserver interface {
	OnTransferStarted(r any, t conductor.FileTransfer)
	OnTransferSuccess(r any, t conductor.FileTransfer)
	OnTransferError(r any, t conductor.FileTransfer, err error)
	OnTransferSkipped(r any, t conductor.FileTransfer, reason string)
}

type LoggingFileRetrievalObserver struct {
	Logger logrus.FieldLogger
}

func (o *LoggingFileRetrievalObserver) OnTransferStarted(r any, t conductor.FileTransfer) {
	o.Logger.Infof("Transfer started [%T]: '%s' -> '%s'", r, t.RemotePath, t.LocalPath)
}

func (o *LoggingFileRetrievalObserver) OnTransferSuccess(r any, t conductor.FileTransfer) {
	o.Logger.Infof("Transfer succeeded [%T]: '%s' -> '%s'", r, t.RemotePath, t.LocalPath)
}

func (o *LoggingFileRetrievalObserver) OnTransferError(r any, t conductor.FileTransfer, err error) {
	o.Logger.Infof("Transfer failed [%T]: '%s' -> '%s': %v", r, t.RemotePath, t.LocalPath, err)
}

func (o *LoggingFileRetrievalObserver) OnTransferSkipped(r any, t conductor.FileTransfer, reason string) {
	o.Logger.Infof("Transfer skipped [%T]: '%s' -> '%s': %s", r, t.RemotePath, t.LocalPath, reason)
}

func (o *LoggingFileRetrievalObserver) OnTransferProgress(r any, t conductor.FileTransfer, received, total int64) {
	o.Logger.Infof("Transfer progress [%T]: '%s' -> '%s': %d/%d KB", r, t.RemotePath, t.LocalPath, received/1024, total/1024)
}

// TransferProgressAggregator aggregates progress across multiple transfers.
// Progress updates are thread safe and reported at most once per interval to prevent overwhelming the receiver.
type TransferProgressAggregator struct {
	transferred int64
	total       int64
	lastUpdate  time.Time
	report      func(received int64, max int64)
	interval    time.Duration
	sync.Mutex
}

// Progress records progress and calls the report function if the interval has elapsed or the transfer is complete.
func (a *TransferProgressAggregator) Progress(received int64) {
	a.Lock()
	transferred := a.transferred + int64(received)
	a.transferred = transferred
	now := time.Now()
	// Ensure we send: on first write, on interval, and at completion.
	update := a.lastUpdate.IsZero() || (transferred >= 0 && transferred >= a.total) || now.Sub(a.lastUpdate) >= a.interval
	if update {
		a.lastUpdate = now
	}
	a.Unlock()

	if update {
		a.report(transferred, a.total)
	}
}

// createStageNotifer creates a TransferProgressAggregator that reports the overall progress of the file transfers to the stage notifier.
func (r *RetrieveAgentFilesStage) createStageNotifer(stageCtx *recipe.StageContext, fileInfos *targetagentproto.ListFilesResponse, transfers []agentFileTransfer) *TransferProgressAggregator {
	aggregateTransferProgress := &TransferProgressAggregator{}
	if stageCtx.StageNotifier != nil {
		// Sum the size of all transfers, ignoring those with errors.
		var transferTotalSize int64
		for i, response := range fileInfos.Responses {
			for _, info := range response.FileInfos {
				if info.IsDir || info.Error != "" {
					continue
				}
				shouldExclude, err := util.MatchesAny(info.Path, transfers[i].Exclude)
				if err != nil || shouldExclude {
					continue
				}
				transferTotalSize += info.Size
			}
		}
		aggregateTransferProgress.total = transferTotalSize
		aggregateTransferProgress.interval = notifiers.ClientFileStreamUpdateInterval
		aggregateTransferProgress.report = func(received int64, max int64) {
			stageCtx.StageNotifier.OnStageProgress(notifiers.StageInfo{Name: r.Name()}, notifiers.StageProgress{
				Sent: received,
				Max:  max,
				Unit: notifiers.UnitBytes,
			})
		}
	}
	return aggregateTransferProgress
}

func (r *RetrieveAgentFilesStage) Execute(stageCtx *recipe.StageContext) (func(), error) {
	baseCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel all transfers when CancelChan fires.
	if stageCtx.CommandStateChannel != nil && stageCtx.CommandStateChannel.CancelChan != nil {
		select {
		case <-stageCtx.CommandStateChannel.CancelChan:
			return nil, message.New(message.EngineCommonUserCancellationError)
		default:
		}

		go func() {
			<-stageCtx.CommandStateChannel.CancelChan
			cancel()
		}()
	}

	retriever, ok := r.RecipeCollector.FileRetriever.(*recipe.RetrieveAgentFilesStageRetriever)
	if !ok {
		err := fmt.Errorf("expected a RetrieveAgentFilesStageRetriever, got %T", r.RecipeCollector.FileRetriever)
		return nil, message.New(message.CommonUnknownError).WithCause(err)
	}

	// Log files always get collected, output files only if the run didn't fail.
	transfers := util.Map(retriever.LogTransfers, func(f recipe.TransferRequest) agentFileTransfer {
		return agentFileTransfer{TransferRequest: f, logFile: true}
	})
	if stageCtx.Failure.Error == nil {
		transfers = append(transfers, util.Map(retriever.FileTransfers, func(f recipe.TransferRequest) agentFileTransfer {
			return agentFileTransfer{TransferRequest: f, logFile: false}
		})...)
	}

	// If nothing to do, return immediately.
	if len(transfers) == 0 {
		return nil, nil
	}

	resolvedTransfers, err := resolveAgentTransfers(transfers)
	if err != nil {
		return nil, message.New(message.CommonUnknownError).WithCause(err)
	}

	fileInfos, err := listFilesByAgent(baseCtx, resolvedTransfers)
	if err != nil {
		return nil, message.New(message.CommonUnknownError).WithCause(err)
	}

	const maxInflight = 4 // make configurable as needed
	sem := semaphore.NewWeighted(int64(maxInflight))
	var wg sync.WaitGroup

	// Error channel: one slot per transfer (+ a little headroom).
	//
	// Since each file transfer could expand to multiple files (globs), we
	// need to drain the error channel in a separate goroutine:
	// - consumer is a single goroutine that drains the channels and saves errors.
	// - producer is the goroutines that retrieves the files via the agent.
	type errorMsg struct {
		err          error
		fileTransfer conductor.FileTransfer
	}
	errCh := make(chan errorMsg, len(transfers)) // at least len(transfers) will be made

	observer := &LoggingFileRetrievalObserver{Logger: logx.FromContext(stageCtx.Context)}

	// Consume the file retrieval errors
	fileRetrievalErrors := map[string]string{}
	errChDone := make(chan struct{})
	go func() {
		defer close(errChDone)

		for msg := range errCh {
			observer.OnTransferError(r, msg.fileTransfer, msg.err)
			logx.FromContext(stageCtx.Context).
				WithError(msg.err).Error("File retrieval error")
			fileRetrievalErrors[msg.fileTransfer.RemotePath] = msg.err.Error()
		}
	}()

	progressNotifier := r.createStageNotifer(stageCtx, fileInfos, transfers)

	for transferI, f := range resolvedTransfers {
		localIsGlob := strings.ContainsRune(f.LocalPath, '*')
		remoteIsGlob := strings.ContainsRune(f.RemotePath, '*')
		remoteDoesntExist := len(fileInfos.Responses[transferI].FileInfos) == 1 && fileInfos.Responses[transferI].FileInfos[0].Error == os.ErrNotExist.Error()
		// It's valid for glob extensions to not map to a file, when this occurs log & skip
		if localIsGlob && remoteIsGlob && remoteDoesntExist {
			observer.OnTransferSkipped(r, f.FileTransfer, "glob expansion has no matches")
			continue
		}

		for _, fi := range fileInfos.Responses[transferI].FileInfos {
			remappedTransfer := conductor.FileTransfer{RemotePath: fi.Path, LocalPath: f.LocalPath}
			exclude, err := util.MatchesAny(fi.Path, f.Exclude)
			if err != nil {
				errCh <- errorMsg{
					err:          fmt.Errorf("error checking if file should be excluded: %q", err),
					fileTransfer: remappedTransfer,
				}
				continue
			}
			if exclude {
				observer.OnTransferSkipped(r, remappedTransfer, "remote path matches exclusion list")
				continue
			}
			if fi.IsDir {
				observer.OnTransferSkipped(r, f.FileTransfer, "directory skipped")
				continue
			}

			observer.OnTransferStarted(r, remappedTransfer)
			transferred := int64(0)

			loggingNotifier := TransferProgressAggregator{
				total:    fi.Size,
				interval: notifiers.LogFileStreamUpdateInterval,
				report: func(received int64, max int64) {
					transferred += received
					if received > 0 && received < max { // Excluded initial and final message as we have existing start/completion messages
						observer.OnTransferProgress(r, remappedTransfer, transferred, fi.Size)
					}
				},
			}

			// Combine the stage notifier with a the logging notifier
			var transferProgress agent.ReportProgress
			if progressNotifier.report != nil {
				multiTrans := MultiTransferProgress{transfers: []TransferProgress{&loggingNotifier, progressNotifier}}
				transferProgress = multiTrans.Progress
			} else {
				transferProgress = loggingNotifier.Progress
			}

			if fi.Error != "" {
				if !f.logFile {
					errCh <- errorMsg{
						err:          fmt.Errorf("file does not exist on target: %q", fi.Error),
						fileTransfer: remappedTransfer,
					}
				}
				continue
			}

			localPath, err := util.RemapGlobbedPath(f.LocalPath, fi.Path, f.RemotePath)
			if err != nil {
				errCh <- errorMsg{
					err:          fmt.Errorf("error remapping globbed path: %q", fi.Error),
					fileTransfer: remappedTransfer,
				}
				continue
			}

			// Acquire slot with cancellation awareness.
			if err := sem.Acquire(baseCtx, 1); err != nil {
				// Likely context cancellation — record and stop launching more.
				errCh <- errorMsg{
					err:          fmt.Errorf("error acquiring semaphore for target (maybe context was cancelled?): %q", err),
					fileTransfer: remappedTransfer,
				}
				break
			}

			wg.Add(1)
			go func(f resolvedAgentFileTransfer) {
				defer wg.Done()
				defer sem.Release(1)
				err := agent.ReceiveFile(baseCtx, localPath, fi.Path, f.agentClient, transferProgress)
				if err != nil {
					errCh <- errorMsg{
						err:          fmt.Errorf("transfer failed %q", err),
						fileTransfer: remappedTransfer,
					}
				} else {
					observer.OnTransferSuccess(r, f.FileTransfer)
				}
			}(f)
		}
	}

	// Wait for all file transfers to be made AND any errors to be consumed
	wg.Wait()
	close(errCh)
	<-errChDone

	// Fail this stage if the run passed and any error was encountered
	// If the run already failed, we ignore retrieval errors to preserve the original failure cause.
	if stageCtx.Failure.Error == nil && len(fileRetrievalErrors) != 0 {
		if len(fileRetrievalErrors) > 1 {
			metadata := fileRetrievalErrors
			metadata["numFiles"] = strconv.Itoa(len(fileRetrievalErrors))
			return nil, message.New(message.EngineRecipeStagesRetrieveAgentFilesRetrieveFiles).WithMetadata(metadata)
		} else {
			metadata := map[string]string{}
			// fileRetrievalErrors only has length 1
			for key := range fileRetrievalErrors {
				metadata["filePath"] = key
				metadata["error"] = fileRetrievalErrors[key]
				// To make certain that we only do this once
				break
			}
			return nil, message.New(message.EngineRecipeStagesRetrieveAgentFilesRetrieveFile).WithMetadata(metadata)
		}
	}
	return nil, nil
}

func (r *RetrieveAgentFilesStage) Name() string {
	return "Retrieving recipe artifacts"
}

func (r *RetrieveAgentFilesStage) ErrorType() run.RunResult {
	return run.RecipeFailureRetrieve
}

func (r *RetrieveAgentFilesStage) AlwaysExecute() bool {
	return true
}

func resolveAgentTransfers(transfers []agentFileTransfer) ([]resolvedAgentFileTransfer, error) {
	resolved := make([]resolvedAgentFileTransfer, len(transfers))
	for i, transfer := range transfers {
		if transfer.AgentSupplier == nil {
			return nil, fmt.Errorf("transfer %q: transfer request has no agent supplier configured", transfer.RemotePath)
		}

		agentConn := transfer.AgentSupplier()
		if agentConn == nil {
			return nil, fmt.Errorf("transfer %q: transfer request agent supplier returned nil agent connection", transfer.RemotePath)
		}
		if agentConn.Client == nil {
			return nil, fmt.Errorf("transfer %q: transfer request agent connection has no client", transfer.RemotePath)
		}

		resolved[i] = resolvedAgentFileTransfer{
			agentFileTransfer: transfer,
			agentClient:       agentConn.Client,
		}
	}
	return resolved, nil
}

// listFilesByAgent batches ListFiles calls per agent and returns
// the responses in the same order as the transfers slice.
func listFilesByAgent(
	ctx context.Context,
	transfers []resolvedAgentFileTransfer,
) (*targetagentproto.ListFilesResponse, error) {
	indicesByClient := map[targetagentproto.TargetAgentClient][]int{}
	for i, transfer := range transfers {
		indicesByClient[transfer.agentClient] = append(indicesByClient[transfer.agentClient], i)
	}

	fileInfos := &targetagentproto.ListFilesResponse{Responses: make([]*targetagentproto.FileInfos, len(transfers))}
	for agentClient, indices := range indicesByClient {
		paths := make([]string, len(indices))
		for i, transferI := range indices {
			paths[i] = transfers[transferI].RemotePath
		}

		response, err := agentClient.ListFiles(ctx, &targetagentproto.ListFilesRequest{Paths: paths})
		if err != nil {
			return nil, err
		}
		// Validate the array sizes match, we'll assume indices correspond after this.
		if len(response.Responses) != len(paths) {
			return nil, fmt.Errorf("expected %d file infos, got %d", len(paths), len(response.Responses))
		}
		for responseI, transferI := range indices {
			fileInfos.Responses[transferI] = response.Responses[responseI]
		}
	}

	return fileInfos, nil
}
