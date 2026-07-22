// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

var stageInfo = notifiers.StageInfo{
	Name: "Retrieving recipe artifacts",
}

type FlushTransfersMessage struct {
	runSucceeded bool
}

type TransferRequest struct {
	conductor.FileTransfer
	// ManifestRelativePath is the original run-manifest path for this transfer request.
	// For globbed transfers this remains the requested "*" path, not any expanded file path.
	ManifestRelativePath string
	AgentSupplier        AgentConnSupplier
	ImmediateRetrieval   bool
	BackgroundTransfer   bool
	completion           *transferRequestCompletion
}

type AddTransferMessage struct {
	t       TransferRequest
	confirm chan struct{}
}

type transferPhase int

const (
	phase1TransferPhase transferPhase = iota
	backgroundTransferPhase
)

func transferPhaseForRequest(req TransferRequest) transferPhase {
	if req.BackgroundTransfer {
		return backgroundTransferPhase
	}
	return phase1TransferPhase
}

func transferPhaseProgressMessage(phase transferPhase) string {
	switch phase {
	case backgroundTransferPhase:
		return "background"
	default:
		return "required"
	}
}

func transferPhaseStageInfo(phase transferPhase) notifiers.StageInfo {
	return notifiers.StageInfo{Name: stageInfo.Name + " (" + transferPhaseProgressMessage(phase) + ")"}
}

// createMultiStageProgressTrackers creates progress trackers for phase1 and background transfers.
func (t *TransferManager) createMultiStageProgressTrackers(notifier notifiers.StageNotifier) *multiPhaseProgressTracker {
	var report func(phase transferPhase, received int64, max int64)
	if notifier != nil {
		report = func(phase transferPhase, received int64, max int64) {
			notifier.OnStageProgress(transferPhaseStageInfo(phase), notifiers.StageProgress{
				Sent: received,
				Max:  max,
				Unit: notifiers.UnitBytes,
			})
		}
	}

	return newMultiPhaseProgressTracker(report)
}

func (t *TransferManager) addTransferCount(phase transferPhase) {
	switch phase {
	case backgroundTransferPhase:
		t.backgroundTransferCount++
	default:
		t.phase1TransferCount++
	}
}

type TransferManager struct {
	ListeningStarted            chan struct{}
	listeningDone               chan struct{}
	transferRequestChannel      chan AddTransferMessage
	flushTransfersChannel       chan FlushTransfersMessage
	transfersCompleteChannel    chan message.Message
	OnPhase1TransferComplete    func()
	phase1TransferCount         int
	backgroundTransferCount     int
	deferredPhase1Transfers     []TransferRequest
	deferredBackgroundTransfers []TransferRequest
	completedTransfers          []conductor.FileTransfer
	completedTransfersMu        sync.Mutex
	baseCtx                     context.Context
	mandatoryCtx                context.Context
	baseWg                      sync.WaitGroup
	phase1Wg                    sync.WaitGroup
	backgroundWg                sync.WaitGroup
	// transferErrors is shared by transfer goroutines. recordTransferError
	// writes to it before the goroutine returns and decrements its phase wait
	// group, so phase wait completion also means phase errors are visible.
	transferErrors *transferErrors
	transferSlots  *transferSlotManager
	// Real notifier is passed in Listen() method
	notifier notifiers.StageNotifier
	observer *LoggingFileRetrievalObserver
	// startedPhaseStages makes phase stage notifications idempotent.
	// Immediate and deferred transfers can both touch the same phase, so a
	// duplicate start or end should be a no-op instead of emitting an invalid
	// notifier lifecycle event.
	startedPhaseStages    map[transferPhase]bool
	stageProgressTrackers *multiPhaseProgressTracker
	manifestUpdater       *run.RunManifestUpdater
}

// startPhaseStage emits the phase stage start notification once. Repeated
// calls for a phase that has already started are no-ops, making phase stage
// starts idempotent.
func (t *TransferManager) startPhaseStage(phase transferPhase) {
	if t.startedPhaseStages[phase] {
		return
	}
	t.notifier.OnStageStart(transferPhaseStageInfo(phase))
	t.startedPhaseStages[phase] = true
}

// endPhaseStage emits the phase stage end notification once. Calls for a phase
// that has not started, or has already ended, are no-ops, making phase stage
// ends idempotent.
func (t *TransferManager) endPhaseStage(phase transferPhase, msg message.Message) {
	if !t.startedPhaseStages[phase] {
		return
	}
	t.notifier.OnStageEnd(transferPhaseStageInfo(phase), msg)
	delete(t.startedPhaseStages, phase)
}

func NewTransferManager(maxInFlightTransfers int, manifestUpdater *run.RunManifestUpdater) *TransferManager {
	return &TransferManager{
		ListeningStarted:            make(chan struct{}),
		listeningDone:               make(chan struct{}),
		transferRequestChannel:      make(chan AddTransferMessage),
		flushTransfersChannel:       make(chan FlushTransfersMessage),
		transfersCompleteChannel:    make(chan message.Message),
		deferredPhase1Transfers:     []TransferRequest{},
		deferredBackgroundTransfers: []TransferRequest{},
		completedTransfers:          []conductor.FileTransfer{},
		completedTransfersMu:        sync.Mutex{},
		transferErrors:              newTransferErrors(),
		transferSlots:               newTransferSlotManager(maxInFlightTransfers),
		startedPhaseStages:          map[transferPhase]bool{},
		manifestUpdater:             manifestUpdater,
		baseWg:                      sync.WaitGroup{},
	}
}

// Listen blocks and listens for transfer requests received by calls to this TransferManager's AddTransfer method.
// It dispatches immediate requests upon receival, and caches deferred requests until FlushTransfers is called.
// Once this happens, deferred requests will be flushed, and this method will return once all transfers have completed.
func (t *TransferManager) Listen(logger logrus.FieldLogger, cmdStateChannel *cmdsync.CommandStateChannel, notifier notifiers.StageNotifier) {
	// Check if this transfer manager is already listening
	select {
	case <-t.ListeningStarted:
		return
	default:
		close(t.ListeningStarted)
	}

	t.observer = &LoggingFileRetrievalObserver{Logger: logger}

	var baseCancel, mandatoryCancel context.CancelFunc
	t.baseCtx, baseCancel = context.WithCancel(context.Background())
	t.mandatoryCtx, mandatoryCancel = context.WithCancel(context.Background())
	allCancel := func() {
		baseCancel()
		mandatoryCancel()
	}
	defer allCancel()

	// Cancel all transfers when CancelChan fires - ignore StopChan
	var cancelChan chan struct{}
	if cmdStateChannel != nil && cmdStateChannel.CancelChan != nil {
		cancelChan = cmdStateChannel.CancelChan
	} else {
		cancelChan = make(chan struct{})
	}
	select {
	case <-cancelChan:
		// Signal that we're done listening and return early
		close(t.listeningDone)
		return
	default:
		ready := make(chan struct{})
		// Kill the watcher when Listen returns
		stopListening := make(chan struct{})
		defer close(stopListening)

		go func() {
			close(ready)
			select {
			case <-cancelChan:
				allCancel()
			case <-stopListening:
			}
		}()
		// Block until we're ready to listen for cancellation requests
		<-ready
	}

	if notifier == nil {
		notifier = &NullStageNotifier{}
	}
	t.stageProgressTrackers = t.createMultiStageProgressTrackers(notifier)
	t.notifier = notifier
	t.observer.OnListen()

	for {
		select {
		case <-cancelChan:
			// Signal that we're done listening, wait for in-progress transfers to be cancelled, and return
			close(t.listeningDone)

			t.phase1Wg.Wait()
			t.backgroundWg.Wait()
			t.removeDeferredPendingManifestEntries()

			t.observer.OnEndListen()
			cancelMessage := message.New(message.EngineCommonUserCancellationError)
			t.endPhaseStage(phase1TransferPhase, cancelMessage)
			t.endPhaseStage(backgroundTransferPhase, cancelMessage)

			return
		case msg := <-t.transferRequestChannel:
			req := msg.t
			phase := transferPhaseForRequest(req)
			t.addTransferCount(phase)
			t.addPendingManifestEntry(req)
			if !req.ImmediateRetrieval {
				if req.BackgroundTransfer {
					t.deferredBackgroundTransfers = append(t.deferredBackgroundTransfers, req)
				} else {
					t.deferredPhase1Transfers = append(t.deferredPhase1Transfers, req)
				}
				t.observer.OnDeferredTransferRequest(req.FileTransfer)
				close(msg.confirm)
				continue
			}

			t.startPhaseStage(phase)
			t.observer.OnImmediateTransferRequest(req.FileTransfer)
			t.startTransferRequest(req)
			close(msg.confirm)
		case flush := <-t.flushTransfersChannel:
			// Block further transfer / flush requests
			close(t.listeningDone)

			t.stageProgressTrackers.SetGoalCounts(t.phase1TransferCount, t.backgroundTransferCount)

			msg := t.flushTransfers(flush, baseCancel)
			select {
			case <-cancelChan:
				msg = message.New(message.EngineCommonUserCancellationError)
			default:
			}

			t.observer.OnEndListen()
			t.transfersCompleteChannel <- msg
			return
		}
	}
}

// AddTransfer enqueues the supplied TransferRequest. If the request has ImmediateRetrieval=true,
// the transfer will begin immediately; otherwise, its execution will be deferred until FlushTransfers
// is called. This method does not wait for the transfer to complete before returning, however it
// _does_ wait for non-deferred transfers to commence (to ensure remote path globs are expanded
// at the point of calling this method). If the TransferManager is not listening, this method will
// return immediately.
func (t *TransferManager) AddTransfer(request TransferRequest) {
	t.addTransfer(request)
}

func (t *TransferManager) addTransfer(request TransferRequest) {
	// Exit early if manager is not available to listen before the message can be sent
	select {
	case <-t.ListeningStarted:
	default:
		request.completion.complete(errors.New("transfer manager has not started listening yet"))
		return
	}

	confirm := make(chan struct{})
	select {
	case t.transferRequestChannel <- AddTransferMessage{
		t:       request,
		confirm: confirm,
	}:
		<-confirm
		return
	case <-t.listeningDone:
		request.completion.complete(errors.New("transfer manager is no longer listening"))
		return
	}
}

// FlushTransfers flushes enqueued transfers. It blocks until all transfers (deferred and immediate)
// have completed, and returns any error that occurred during the transfers. Once this method is called,
// the TransferManager will ignore future AddTransfer / Flush requests. If the TransferManager is not
// listening, this method will return immediately with an error.
func (t *TransferManager) FlushTransfers(runSucceeded bool) message.Message {
	// Exit early if manager is not available to listen before the message can be sent
	select {
	case <-t.ListeningStarted:
	default:
		return message.New(message.CommonUnknownError).WithCause(errors.New("TransferManager has not started listening yet"))
	}

	select {
	case t.flushTransfersChannel <- FlushTransfersMessage{runSucceeded: runSucceeded}:
		return <-t.transfersCompleteChannel
	case <-t.listeningDone:
		return message.New(message.CommonUnknownError).WithCause(errors.New("TransferManager is no longer listening; FlushTransfers might have already been called, or a cancellation request may have been received"))
	}
}

// flushTransfers is an internal method that responds to a FlushTransfersMessage. If the run succeeded,
// it simply enqueues all deferred transfers for execution, waits for them all to complete, and then
// returns an error if any failed. If the run failed, it cancels any in-progress non-mandatory
// transfers, reverts any completed non-mandatory transfers, and only enqueues mandatory deferred
// transfers - again, it waits for them all to complete and returns an error.
func (t *TransferManager) flushTransfers(flush FlushTransfersMessage, baseCancel context.CancelFunc) message.Message {
	numDeferredTransfers := len(t.deferredPhase1Transfers) + len(t.deferredBackgroundTransfers)
	t.observer.OnTransfersFlushed(numDeferredTransfers)
	if !flush.runSucceeded {
		// Only cancel non-mandatory transfers in this case
		baseCancel()
		// Wait for cancellation to propagate before rolling back transfers
		t.baseWg.Wait()
		for _, transfer := range t.getCompletedTransfersCopy() {
			if isTransferMandatory(transfer) {
				continue
			}
			t.rollbackTransfer(transfer)
		}
	}

	t.startPhaseStage(phase1TransferPhase)
	t.flushPhase1DeferredTransfers(flush)
	t.phase1Wg.Wait()
	phase1Msg := t.transferErrorMessage(flush, t.transferErrors.phase1Copy())
	if flush.runSucceeded && phase1Msg == nil && t.OnPhase1TransferComplete != nil {
		t.OnPhase1TransferComplete()
	}
	t.endPhaseStage(phase1TransferPhase, phase1Msg)

	if t.shouldSkipBackgroundTransfersAfterPhase1(flush) {
		t.removeDeferredPendingManifestEntries()
	} else {

		// We always show the "required" progress bar, but only show the background progress bar if we have any transfers
		if t.backgroundTransferCount > 0 {
			t.startPhaseStage(backgroundTransferPhase)
		}
		t.stageProgressTrackers.SetBackground()
		t.flushBackgroundDeferredTransfers(flush)
	}
	t.backgroundWg.Wait()
	backgroundMsg := t.transferErrorMessage(flush, t.transferErrors.backgroundCopy())
	t.endPhaseStage(backgroundTransferPhase, backgroundMsg)

	return t.transferErrorMessage(flush, t.transferErrors.combined())
}

func (t *TransferManager) flushPhase1DeferredTransfers(flush FlushTransfersMessage) {
	globbedTransferWg := sync.WaitGroup{}
	for _, request := range t.deferredPhase1Transfers {
		// Start each transfer in a separate goroutine to parallelise listing files on the target - this
		// minimises the time it takes for us to make the CLI progress bar determinate
		globbedTransferWg.Add(1)
		go func(request TransferRequest) {
			t.flushTransfer(flush, request)
			globbedTransferWg.Done()
		}(request)
	}
	// Wait for all transfer groups to start
	globbedTransferWg.Wait()
}

func (t *TransferManager) flushBackgroundDeferredTransfers(flush FlushTransfersMessage) {
	globbedTransferWg := sync.WaitGroup{}
	for _, request := range t.deferredBackgroundTransfers {
		globbedTransferWg.Add(1)
		go func(request TransferRequest) {
			t.flushTransfer(flush, request)
			globbedTransferWg.Done()
		}(request)
	}
	globbedTransferWg.Wait()
}

func (t *TransferManager) removeDeferredPendingManifestEntries() {
	for _, request := range t.deferredPhase1Transfers {
		err := t.manifestUpdater.RemovePendingComponent(request.ManifestRelativePath)
		if err != nil {
			t.recordTransferError(fmt.Errorf("failed to remove pending manifest entry: %w", err), request.FileTransfer, transferPhaseForRequest(request))
		}
	}
	for _, request := range t.deferredBackgroundTransfers {
		err := t.manifestUpdater.RemovePendingComponent(request.ManifestRelativePath)
		if err != nil {
			t.recordTransferError(fmt.Errorf("failed to remove pending manifest entry: %w", err), request.FileTransfer, transferPhaseForRequest(request))
		}
	}
}

func (t *TransferManager) shouldSkipBackgroundTransfersAfterPhase1(flush FlushTransfersMessage) bool {
	return flush.runSucceeded && t.baseCtx != nil && t.baseCtx.Err() != nil
}

func (t *TransferManager) flushTransfer(flush FlushTransfersMessage, request TransferRequest) {
	transfer := request.FileTransfer
	phase := transferPhaseForRequest(request)
	if !flush.runSucceeded && !isTransferMandatory(transfer) {
		// Increment the observer transfer count, but with 0 bytes to transfer (since it was skipped)
		t.stageProgressTrackers.GetFromPhase(phase).AddBytesToTotal(0)
		t.finishTransferRequest(request, false)
		return
	}
	// Note this is not in a separate goroutine, because each definite transfer that this (potentially) globbed
	// transfer expands to will be started in a new goroutine within startTransferRequest - i.e. this call may return before
	// all transfers within it are complete
	t.startTransferRequest(request)
}

func (t *TransferManager) transferErrorMessage(flush FlushTransfersMessage, fileRetrievalErrors map[transferErrorKey]error) message.Message {
	// Fail this stage if the run passed and any error was encountered
	// If the run already failed, we ignore retrieval errors to preserve the original failure cause.
	if flush.runSucceeded && len(fileRetrievalErrors) > 0 {
		if len(fileRetrievalErrors) > 1 {
			metadata := transferErrorMetadata(fileRetrievalErrors)
			metadata["numFiles"] = strconv.Itoa(len(fileRetrievalErrors))
			causes := make([]error, 0, len(fileRetrievalErrors))
			for _, err := range fileRetrievalErrors {
				causes = append(causes, err)
			}
			return message.New(message.EngineRecipeStagesRetrieveAgentFilesRetrieveFiles).
				WithMetadata(metadata).
				WithCause(errors.Join(causes...))
		} else {
			metadata := map[string]string{}
			var cause error
			// fileRetrievalErrors only has length 1
			for key, err := range fileRetrievalErrors {
				metadata["filePath"] = key.remotePath
				metadata["error"] = err.Error()
				cause = err
				// To make certain that we only do this once
				break
			}
			return message.New(message.EngineRecipeStagesRetrieveAgentFilesRetrieveFile).WithMetadata(metadata).WithCause(cause)
		}
	}

	return nil
}

func (t *TransferManager) startTransferRequest(request TransferRequest) {
	transfer := request.FileTransfer
	phase := transferPhaseForRequest(request)

	var transferCtx context.Context
	var wg *sync.WaitGroup
	if isTransferMandatory(transfer) {
		transferCtx = t.mandatoryCtx
	} else {
		transferCtx = t.baseCtx
		wg = &t.baseWg
	}

	fileInfoRequest := []string{transfer.RemotePath}
	agentConn, err := resolveTransferAgentConn(request)
	if err != nil {
		t.recordTransferError(message.New(message.CommonUnknownError).WithCause(err), transfer, phase)
		t.finishTransferRequest(request, false)
		t.stageProgressTrackers.GetFromPhase(phase).AddBytesToTotal(0)
		return
	}
	fileInfos, err := agentConn.Client.ListFiles(transferCtx, &targetagentproto.ListFilesRequest{Paths: fileInfoRequest})
	if err != nil {
		t.recordTransferError(message.New(message.CommonUnknownError).WithCause(err), transfer, phase)
		t.finishTransferRequest(request, false)
		t.stageProgressTrackers.GetFromPhase(phase).AddBytesToTotal(0)
		return
	} else if len(fileInfos.Responses) != 1 {
		t.recordTransferError(
			message.New(message.CommonUnknownError).WithCause(fmt.Errorf("expected 1 file info, got %d", len(fileInfos.Responses))),
			transfer,
			phase,
		)
		t.finishTransferRequest(request, false)
		t.stageProgressTrackers.GetFromPhase(phase).AddBytesToTotal(0)
		return
	}
	fileInfo := fileInfos.Responses[0]

	localIsGlob := strings.ContainsRune(transfer.LocalPath, '*')
	remoteIsGlob := strings.ContainsRune(transfer.RemotePath, '*')
	remoteDoesntExist := len(fileInfo.FileInfos) == 1 && fileInfo.FileInfos[0].Error == os.ErrNotExist.Error()
	// It's valid for glob extensions to not map to a file, when this occurs log & skip
	if localIsGlob && remoteIsGlob && remoteDoesntExist {
		t.observer.OnTransferSkipped(transfer, "glob expansion has no matches")
		t.finishTransferRequest(request, false)
		t.stageProgressTrackers.GetFromPhase(phase).AddBytesToTotal(0)
		return
	}

	filteredInfos := []*targetagentproto.FileInfo{}
	// Filter out transfers that won't be executed, and sum the sizes of the rest
	var transferTotalSize int64
	requestFailed := false
	for _, info := range fileInfo.FileInfos {
		infoTransfer := conductor.FileTransfer{RemotePath: info.Path, LocalPath: transfer.LocalPath, ComponentType: transfer.ComponentType}
		if info.IsDir {
			t.observer.OnTransferSkipped(infoTransfer, "directory skipped")
			continue
		} else if info.Error != "" {
			if !isTransferMandatory(transfer) {
				t.recordTransferError(fmt.Errorf("file does not exist on target: %q", info.Error), infoTransfer, phase)
			}
			requestFailed = true
			continue
		}

		exclude, err := util.MatchesAny(info.Path, transfer.Exclude)
		if err != nil {
			t.recordTransferError(fmt.Errorf("error checking if file should be excluded: %w", err), infoTransfer, phase)
			requestFailed = true
			continue
		}
		if exclude {
			t.observer.OnTransferSkipped(infoTransfer, "remote path matches exclusion list")
			continue
		}

		transferTotalSize += info.Size
		filteredInfos = append(filteredInfos, info)
	}
	t.stageProgressTrackers.GetFromPhase(phase).AddBytesToTotal(transferTotalSize)

	requestCompletion := t.newTransferRequestCompletionTracker(request, len(filteredInfos), requestFailed)

	if len(filteredInfos) == 0 {
		// If no files (i.e directory-only), remove entry from manifest.
		t.finishTransferRequest(request, false)
	}
	for _, fi := range filteredInfos {
		localPath, err := util.RemapGlobbedPath(transfer.LocalPath, fi.Path, transfer.RemotePath)
		if err != nil {
			remapTransfer := conductor.FileTransfer{RemotePath: fi.Path, LocalPath: transfer.LocalPath, ComponentType: transfer.ComponentType}
			t.recordTransferError(fmt.Errorf("error remapping globbed path: %w", err), remapTransfer, phase)
			requestCompletion.done(false)
			continue
		}

		resolvedTransfer := conductor.FileTransfer{RemotePath: fi.Path, LocalPath: localPath, ComponentType: transfer.ComponentType}

		t.startConcreteTransfer(transferCtx, wg, phase, resolvedTransfer, fi.Size, agentConn.Client, requestCompletion.done)
	}
}

func resolveTransferAgentConn(request TransferRequest) (*agent.AgentConn, error) {
	if request.AgentSupplier == nil {
		return nil, errors.New("transfer request has no agent supplier configured")
	}

	agentConn := request.AgentSupplier()
	if agentConn == nil {
		return nil, errors.New("transfer request agent supplier returned nil agent connection")
	}
	if agentConn.Client == nil {
		return nil, errors.New("transfer request agent connection has no client")
	}

	return agentConn, nil
}

// transferRequestCompletionTracker finalizes one manifest request after all
// concrete transfers for that request finish.
type transferRequestCompletionTracker struct {
	t         *TransferManager
	request   TransferRequest
	total     int
	completed int
	failed    bool
	mu        sync.Mutex
}

func (t *TransferManager) newTransferRequestCompletionTracker(request TransferRequest, total int, failed bool) *transferRequestCompletionTracker {
	return &transferRequestCompletionTracker{t: t, request: request, total: total, failed: failed}
}

// done records one concrete transfer result and finalizes the manifest entry when
// every concrete file for the original request has reached a terminal state.
func (c *transferRequestCompletionTracker) done(success bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !success {
		c.failed = true
	}
	c.completed++
	if c.completed != c.total {
		return
	}
	c.t.finishTransferRequest(c.request, !c.failed)
}

func (t *TransferManager) addPendingManifestEntry(request TransferRequest) {
	if request.ManifestRelativePath == "" {
		return
	}
	err := t.manifestUpdater.AddPendingComponent(request.ManifestRelativePath, request.ComponentType)
	if err != nil {
		t.recordTransferError(fmt.Errorf("failed to write pending manifest entry: %w", err), request.FileTransfer, transferPhaseForRequest(request))
	}
}

// finishManifestRequest applies the final manifest state for one original request.
// Successful requests clear pending; failed, cancelled, skipped, or no-artifact
// requests remove the pending entry.
func (t *TransferManager) finishManifestRequest(request TransferRequest, artifactComplete bool) {
	if request.ManifestRelativePath == "" {
		return
	}
	var err error
	if artifactComplete {
		err = t.manifestUpdater.ClearPending(request.ManifestRelativePath)
	} else {
		err = t.manifestUpdater.RemoveComponent(request.ManifestRelativePath)
	}
	if err != nil {
		t.recordTransferError(fmt.Errorf("failed to update manifest entry: %w", err), request.FileTransfer, transferPhaseForRequest(request))
	}
}

func (t *TransferManager) finishTransferRequest(request TransferRequest, artifactComplete bool) {
	t.finishManifestRequest(request, artifactComplete)
	if artifactComplete {
		request.completion.complete(nil)
		return
	}
	request.completion.complete(fmt.Errorf("transfer did not complete: %s", request.RemotePath))
}

// startConcreteTransfer starts one concrete transfer in a goroutine. The caller has already
// resolved globs, directories, and target-side file metadata; this method handles transfer
// slot acquisition, progress reporting, completion tracking, and error recording.
func (t *TransferManager) startConcreteTransfer(
	transferCtx context.Context,
	wg *sync.WaitGroup,
	phase transferPhase,
	resolvedTransfer conductor.FileTransfer,
	fileSize int64,
	agentClient targetagentproto.TargetAgentClient,
	onComplete func(bool),
) {
	specificTracker := NewSpecificTransferTracker(t.observer, resolvedTransfer, fileSize)
	// Combine the stage-wide notifier with the transfer-specific logging notifier
	multiTrans := MultiTransferProgress{transfers: []TransferProgress{&specificTracker, t.stageProgressTrackers.GetFromPhase(phase)}}
	transferProgress := multiTrans.Progress

	if wg != nil {
		wg.Add(1)
	}
	t.addPhaseWaitGroup(phase)
	go func() {
		success := false
		if wg != nil {
			defer wg.Done()
		}
		defer t.donePhaseWaitGroup(phase)
		defer func() {
			if onComplete != nil {
				onComplete(success)
			}
		}()

		err := t.transferSlots.Acquire(transferCtx, phase)
		if err != nil {
			t.recordTransferError(
				fmt.Errorf("error acquiring transfer slot for target (maybe context was cancelled?): %w", err),
				resolvedTransfer,
				phase,
			)
			return
		}
		defer func() {
			if err := t.transferSlots.Release(); err != nil {
				t.recordTransferError(err, resolvedTransfer, phase)
				success = false
			}
		}()

		t.observer.OnTransferStarted(resolvedTransfer)
		err = agent.ReceiveFile(transferCtx, resolvedTransfer.LocalPath, resolvedTransfer.RemotePath, agentClient, transferProgress)
		if err != nil {
			t.recordTransferError(fmt.Errorf("transfer failed %w", err), resolvedTransfer, phase)
		} else {
			t.observer.OnTransferSuccess(resolvedTransfer)
			t.addCompletedTransfer(resolvedTransfer)
			success = true
		}
	}()
}

func (t *TransferManager) recordTransferError(err error, transfer conductor.FileTransfer, phase transferPhase) {
	t.observer.OnTransferError(transfer, err)
	t.transferErrors.add(phase, transfer, err)
}

func (t *TransferManager) addPhaseWaitGroup(phase transferPhase) {
	switch phase {
	case backgroundTransferPhase:
		t.backgroundWg.Add(1)
	default:
		t.phase1Wg.Add(1)
	}
}

func (t *TransferManager) donePhaseWaitGroup(phase transferPhase) {
	switch phase {
	case backgroundTransferPhase:
		t.backgroundWg.Done()
	default:
		t.phase1Wg.Done()
	}
}

// rollbackTransfer attempts to roll back the specified transfer by deleting the file
// at the local path.
func (t *TransferManager) rollbackTransfer(transfer conductor.FileTransfer) {
	t.observer.OnTransferRollback(transfer)
	err := os.Remove(transfer.LocalPath)
	if err != nil {
		t.observer.OnRollbackError(transfer, err)
	}
}

func (t *TransferManager) addCompletedTransfer(transfer conductor.FileTransfer) {
	t.completedTransfersMu.Lock()
	defer t.completedTransfersMu.Unlock()

	t.completedTransfers = append(t.completedTransfers, transfer)
}

func (t *TransferManager) getCompletedTransfersCopy() []conductor.FileTransfer {
	t.completedTransfersMu.Lock()
	defer t.completedTransfersMu.Unlock()

	return append([]conductor.FileTransfer{}, t.completedTransfers...)
}
func isTransferMandatory(transfer conductor.FileTransfer) bool {
	return cdf.IsLogComponentType(transfer.ComponentType)
}

type transferRequestCompletion struct {
	done chan error
	once sync.Once
}

func newTransferRequestCompletion() *transferRequestCompletion {
	return &transferRequestCompletion{done: make(chan error, 1)}
}

func (c *transferRequestCompletion) wait() error {
	return <-c.done
}

func (c *transferRequestCompletion) complete(err error) {
	if c == nil {
		return
	}
	c.once.Do(func() {
		c.done <- err
		close(c.done)
	})
}

func (t *TransferManager) CopyFromTargetAndWait(transfer conductor.FileTransfer, agentSupplier AgentConnSupplier) error {
	completion := newTransferRequestCompletion()
	t.addTransfer(TransferRequest{
		FileTransfer:       transfer,
		AgentSupplier:      agentSupplier,
		ImmediateRetrieval: true,
		completion:         completion,
	})
	return completion.wait()
}
