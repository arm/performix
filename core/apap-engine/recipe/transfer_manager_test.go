// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	agentmocks "github.com/Arm-Debug/apap-cli/apap-engine/agent/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	enginemocks "github.com/Arm-Debug/apap-cli/apap-engine/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	runmocks "github.com/Arm-Debug/apap-cli/apap-engine/run/mocks"
	targetagentmocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

const (
	testRemotePath     = "/remote/output.txt"
	testLocalFileName  = "output.txt"
	testGlobRemotePath = "/remote/*"
	testFileContents   = "file contents"
)

type progressRecordingNotifier struct {
	NullStageNotifier
	progress chan notifiers.StageProgress
	events   chan string
}

func (n *progressRecordingNotifier) OnStageProgress(stageInfo notifiers.StageInfo, progress notifiers.StageProgress) {
	n.progress <- progress
	if n.events != nil {
		n.events <- "progress:" + stageInfo.Name
	}
}

type stageLifecycleRecordingNotifier struct {
	NullStageNotifier
	events []string
}

func (n *stageLifecycleRecordingNotifier) OnStageStart(stageInfo notifiers.StageInfo) {
	n.events = append(n.events, "start:"+stageInfo.Name)
}

func (n *stageLifecycleRecordingNotifier) OnStageEnd(stageInfo notifiers.StageInfo, err error) {
	n.events = append(n.events, "end:"+stageInfo.Name)
}

func fileTransfer(t *testing.T, mandatory bool) conductor.FileTransfer {
	t.Helper()
	transfer := conductor.FileTransfer{
		RemotePath: testRemotePath,
		LocalPath:  filepath.Join(t.TempDir(), testLocalFileName),
	}
	if mandatory {
		transfer.ComponentType = cdf.ComponentType{Name: cdf.TypeLogText}
	}
	return transfer
}

func globFileTransfer(t *testing.T) conductor.FileTransfer {
	t.Helper()
	return conductor.FileTransfer{
		RemotePath: testGlobRemotePath,
		LocalPath:  filepath.Join(t.TempDir(), "out", "*"),
	}
}

func agentSupplier(client *targetagentmocks.TargetAgentClient) func() *agent.AgentConn {
	return func() *agent.AgentConn { return &agent.AgentConn{Client: client} }
}

func newTransferManagerWithLogBuffer() (*TransferManager, *bytes.Buffer) {
	var buf bytes.Buffer
	logger := logrus.New()
	logger.SetOutput(&buf)
	logger.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})

	builder := &run.RunBuilder{}
	runWriter := &runmocks.MockRunWriter{}
	runWriter.On("WriteManifest", mock.Anything).Return(nil)
	runWriter.On("WriteEntityDirs", mock.Anything).Return(nil)

	return &TransferManager{
		observer:           &LoggingFileRetrievalObserver{Logger: logger},
		notifier:           &NullStageNotifier{},
		startedPhaseStages: map[transferPhase]bool{},
		manifestUpdater:    run.NewRunManifestUpdater(builder, runWriter),
	}, &buf
}

func newStartTransferTestManager(
	t *testing.T,
	client *targetagentmocks.TargetAgentClient,
	maxInFlightTransfers int64,
) (*TransferManager, *bytes.Buffer, context.CancelFunc, context.CancelFunc) {
	t.Helper()

	tm, logBuf := newTransferManagerWithLogBuffer()
	baseCtx, baseCancel := context.WithCancel(context.Background())
	mandatoryCtx, mandatoryCancel := context.WithCancel(context.Background())
	t.Cleanup(baseCancel)
	t.Cleanup(mandatoryCancel)

	tm.baseCtx = baseCtx
	tm.mandatoryCtx = mandatoryCtx
	tm.baseWg = sync.WaitGroup{}
	tm.transferErrors = newTransferErrors()
	tm.transferSlots = newTransferSlotManager(int(maxInFlightTransfers))
	tm.stageProgressTrackers = newMultiPhaseProgressTracker(func(transferPhase, int64, int64) {})

	return tm, logBuf, baseCancel, mandatoryCancel
}

func listFilesResponse(fileInfos ...*targetagentproto.FileInfo) *targetagentproto.ListFilesResponse {
	return &targetagentproto.ListFilesResponse{Responses: []*targetagentproto.FileInfos{
		{FileInfos: fileInfos},
	}}
}

func mockListFiles(
	client *targetagentmocks.TargetAgentClient,
	remotePath string,
	response *targetagentproto.ListFilesResponse,
) {
	client.On(
		"ListFiles",
		mock.Anything,
		mock.MatchedBy(func(req *targetagentproto.ListFilesRequest) bool {
			return len(req.Paths) == 1 && req.Paths[0] == remotePath
		}),
	).Return(response, nil).Once()
}

func mockSingleListFiles(
	client *targetagentmocks.TargetAgentClient,
	remotePath string,
) {
	mockListFiles(client, remotePath, listFilesResponse(&targetagentproto.FileInfo{
		Path: remotePath,
		Size: int64(len(testFileContents)),
	}))
}

func mockRetrieveFile(
	client *targetagentmocks.TargetAgentClient,
	remotePath string,
) {
	mockRetrieveFileWithContents(client, remotePath, testFileContents)
}

func mockRetrieveFileWithContents(
	client *targetagentmocks.TargetAgentClient,
	remotePath string,
	fileContents string,
) {
	stream := &agentmocks.MockRetrieveFileStream{}
	agentmocks.SetStreamRecv(stream, fileContents, nil)
	agentmocks.SetStreamRecv(stream, "", io.EOF)

	client.On(
		"RetrieveFile",
		mock.Anything,
		mock.MatchedBy(func(req *targetagentproto.FileRequest) bool {
			return req.Path == remotePath
		}),
		mock.Anything,
	).Return(stream, nil).Once()
}

func mockRetrieveFileError(
	client *targetagentmocks.TargetAgentClient,
	remotePath string,
	err error,
) {
	stream := &agentmocks.MockRetrieveFileStream{}
	stream.On("RecvMsg", mock.Anything).Return(err).Once()

	client.On(
		"RetrieveFile",
		mock.Anything,
		&targetagentproto.FileRequest{Path: remotePath},
		mock.Anything,
	).Return(stream, nil).Once()
}

func requireNoTransferErrors(t *testing.T, tm *TransferManager) {
	t.Helper()
	assert.Empty(t, tm.transferErrors.combined())
}

func transferErrorPath(transfer conductor.FileTransfer) string {
	return newTransferErrorKey(transfer).String()
}

func requireTransferError(t *testing.T, tm *TransferManager, phase transferPhase, transfer conductor.FileTransfer) string {
	t.Helper()

	var errs map[transferErrorKey]error
	switch phase {
	case backgroundTransferPhase:
		errs = tm.transferErrors.backgroundCopy()
	default:
		errs = tm.transferErrors.phase1Copy()
	}

	key := newTransferErrorKey(transfer)
	got, ok := errs[key]
	assert.Truef(t, ok, "expected transfer error for %q, got %#v", key.String(), errs)
	return got.Error()
}

func requireFileContents(t *testing.T, path string, expected string) {
	t.Helper()

	gotContents, err := os.ReadFile(path)
	assert.NoError(t, err)
	assert.Equal(t, expected, string(gotContents))
}

func logText(expected string) string {
	quoted := strconv.Quote(expected)
	return quoted[1 : len(quoted)-1]
}

func requireLogContains(t *testing.T, logOutput string, expected string) {
	t.Helper()

	assert.Contains(t, logOutput, logText(expected))
}

func newManifestTestTransferManager(t *testing.T, client *targetagentmocks.TargetAgentClient) (*TransferManager, *run.RunCollection, run.RunID) {
	t.Helper()

	tm, _, _, _ := newStartTransferTestManager(t, client, 4)
	runCollection, err := run.NewRunCollection(t.TempDir())
	require.NoError(t, err)
	builder, err := runCollection.RunBuilder()
	require.NoError(t, err)
	_, err = runCollection.CreateRun(builder, &cdf.Metadata{})
	require.NoError(t, err)
	runWriter := &run.ConcreteRunWriter{RunCollection: runCollection}
	tm.manifestUpdater = run.NewRunManifestUpdater(&builder, runWriter)
	return tm, runCollection, builder.RunID()
}

func requirePersistedManifestEntry(t *testing.T, runCollection *run.RunCollection, runID run.RunID, path string) *cdf.ManifestEntry {
	t.Helper()

	model, err := runCollection.LoadRun(runID)
	require.NoError(t, err)
	manifest := model.Manifest()
	entry := manifest.Lookup(path)
	require.NotNil(t, entry)
	return entry
}

func requireNoPersistedManifestEntry(t *testing.T, runCollection *run.RunCollection, runID run.RunID, path string) {
	t.Helper()

	model, err := runCollection.LoadRun(runID)
	require.NoError(t, err)
	manifest := model.Manifest()
	require.Nil(t, manifest.Lookup(path))
}

func waitGroupDone(wg *sync.WaitGroup) chan struct{} {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	return done
}

func requireWaitGroupDone(t *testing.T, wg *sync.WaitGroup) {
	requireMessage(t, waitGroupDone(wg), "expected wait group to be done")
}

func requireWaitGroupBlocked(t *testing.T, wg *sync.WaitGroup) {
	requireNoMessage(t, waitGroupDone(wg), func(got struct{}) string {
		return "expected wait group to still be waiting"
	})
}

func requireMessage[T any](t *testing.T, ch <-chan T, msg string) T {
	t.Helper()

	var empty T
	select {
	case got := <-ch:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal(msg)
	}
	return empty
}

func requireNoMessage[T any](t *testing.T, ch <-chan T, msg func(got T) string) {
	t.Helper()

	select {
	case got := <-ch:
		t.Fatal(msg(got))
	case <-time.After(25 * time.Millisecond):
	}
}

func TestTransferManagerListen(t *testing.T) {
	waitForFlushResult := func(t *testing.T, resultCh chan message.Message) message.Message {
		return requireMessage(t, resultCh, "FlushTransfers did not return")
	}

	requireFlushNoLongerListening := func(t *testing.T, tm *TransferManager, runSucceeded bool) {
		t.Helper()

		resultCh := make(chan message.Message, 1)
		go func() {
			resultCh <- tm.FlushTransfers(runSucceeded)
		}()

		got := waitForFlushResult(t, resultCh)
		expected := message.New(message.CommonUnknownError)
		assert.ErrorIs(t, got, expected)
		assert.Contains(t, got.Error(), "TransferManager is no longer listening")
	}

	newListenTestManager := func(
		client *targetagentmocks.TargetAgentClient,
		maxInFlightTransfers int64,
	) (*TransferManager, *bytes.Buffer) {
		tm, logBuf := newTransferManagerWithLogBuffer()
		tm.ListeningStarted = make(chan struct{})
		tm.listeningDone = make(chan struct{})
		tm.transferRequestChannel = make(chan AddTransferMessage)
		tm.flushTransfersChannel = make(chan FlushTransfersMessage)
		tm.transfersCompleteChannel = make(chan message.Message, 1)
		tm.transferErrors = newTransferErrors()
		tm.transferSlots = newTransferSlotManager(int(maxInFlightTransfers))
		tm.stageProgressTrackers = newMultiPhaseProgressTracker(func(transferPhase, int64, int64) {})

		return tm, logBuf
	}

	setupMockedRetrieveFileManager := func() (*TransferManager, conductor.FileTransfer, chan struct{}, *bytes.Buffer, *targetagentmocks.TargetAgentClient) {
		client := &targetagentmocks.TargetAgentClient{}
		tm, logBuf := newListenTestManager(client, 1)
		transfer := fileTransfer(t, false)
		mockSingleListFiles(client, transfer.RemotePath)

		retrieveFileCalled := make(chan struct{}, 1)
		retrieveStream := &agentmocks.MockRetrieveFileStream{}
		retrieveStream.On("RecvMsg", mock.Anything).Run(func(args mock.Arguments) {
			args.Get(0).(*targetagentproto.FileContent).Content = []byte(testFileContents)
		}).Return(nil).Once()
		retrieveStream.On("RecvMsg", mock.Anything).Return(io.EOF).Once()

		client.On("RetrieveFile", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			retrieveFileCalled <- struct{}{}
		}).Return(retrieveStream, nil).Once()

		go tm.Listen(tm.observer.Logger, nil, &NullStageNotifier{})
		<-tm.ListeningStarted

		return tm, transfer, retrieveFileCalled, logBuf, client
	}

	mockBlockingRetrieveFile := func(
		client *targetagentmocks.TargetAgentClient,
		remotePath string,
		finishSuccessfully bool,
	) (chan struct{}, chan struct{}) {
		retrieveFileCalled := make(chan struct{}, 1)
		releaseTransfer := make(chan struct{})
		stream := &agentmocks.MockRetrieveFileStream{}
		recvErr := error(nil)
		if !finishSuccessfully {
			recvErr = context.Canceled
		}
		stream.On("RecvMsg", mock.Anything).Run(func(args mock.Arguments) {
			retrieveFileCalled <- struct{}{}
			<-releaseTransfer
			if finishSuccessfully {
				args.Get(0).(*targetagentproto.FileContent).Content = []byte(testFileContents)
			}
		}).Return(recvErr).Once()
		if finishSuccessfully {
			stream.On("RecvMsg", mock.Anything).Return(io.EOF).Once()
		}

		client.On(
			"RetrieveFile",
			mock.Anything,
			&targetagentproto.FileRequest{Path: remotePath},
			mock.Anything,
		).Return(stream, nil).Once()

		return retrieveFileCalled, releaseTransfer
	}

	mockContextCancelledRetrieveFile := func(
		client *targetagentmocks.TargetAgentClient,
		remotePath string,
	) chan struct{} {
		retrieveFileCalled := make(chan struct{}, 1)
		streamCtx := make(chan context.Context, 1)
		stream := &agentmocks.MockRetrieveFileStream{}
		stream.On("RecvMsg", mock.Anything).Run(func(args mock.Arguments) {
			ctx := <-streamCtx
			<-ctx.Done()
		}).Return(errors.New("transfer cancelled")).Once()
		client.On(
			"RetrieveFile",
			mock.Anything,
			&targetagentproto.FileRequest{Path: remotePath},
			mock.Anything,
		).Run(func(args mock.Arguments) {
			streamCtx <- args.Get(0).(context.Context)
			retrieveFileCalled <- struct{}{}
		}).Return(stream, nil).Once()

		return retrieveFileCalled
	}

	t.Run("closes confirm channel after deferred transfer is enqueued", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _ := newListenTestManager(client, 1)
		transfer := fileTransfer(t, false)
		confirm := make(chan struct{})

		go tm.Listen(tm.observer.Logger, nil, &NullStageNotifier{})
		<-tm.ListeningStarted

		tm.transferRequestChannel <- AddTransferMessage{
			t:       TransferRequest{FileTransfer: transfer},
			confirm: confirm,
		}

		requireMessage(t, confirm, "confirm channel was not closed after deferred transfer was enqueued")
		require.Equal(t, []TransferRequest{{FileTransfer: transfer}}, tm.deferredPhase1Transfers)

		got := tm.FlushTransfers(false)
		assert.NoError(t, got)
		client.AssertExpectations(t)
	})

	t.Run("closes confirm channel after immediate transfer is started", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _ := newListenTestManager(client, 1)
		transfer := fileTransfer(t, false)
		confirm := make(chan struct{})
		listFilesCalled := make(chan struct{}, 1)
		releaseListFiles := make(chan struct{})

		client.On(
			"ListFiles",
			mock.Anything,
			mock.MatchedBy(func(req *targetagentproto.ListFilesRequest) bool {
				return len(req.Paths) == 1 && req.Paths[0] == transfer.RemotePath
			}),
		).Run(func(args mock.Arguments) {
			listFilesCalled <- struct{}{}
			<-releaseListFiles
		}).Return(listFilesResponse(&targetagentproto.FileInfo{
			Path: transfer.RemotePath,
			Size: int64(len(testFileContents)),
		}), nil).Once()

		retrieveStream := &agentmocks.MockRetrieveFileStream{}
		retrieveStream.On("RecvMsg", mock.Anything).Run(func(args mock.Arguments) {
			args.Get(0).(*targetagentproto.FileContent).Content = []byte(testFileContents)
		}).Return(nil).Once()
		retrieveStream.On("RecvMsg", mock.Anything).Return(io.EOF).Once()

		client.On(
			"RetrieveFile",
			mock.Anything,
			&targetagentproto.FileRequest{Path: transfer.RemotePath},
			mock.Anything,
		).Return(retrieveStream, nil).Once()

		go tm.Listen(tm.observer.Logger, nil, &NullStageNotifier{})
		<-tm.ListeningStarted

		go func() {
			tm.transferRequestChannel <- AddTransferMessage{
				t:       TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client), ImmediateRetrieval: true},
				confirm: confirm,
			}
		}()

		requireMessage(t, listFilesCalled, "ListFiles was not called")
		requireNoMessage(t, confirm, func(got struct{}) string {
			return "confirm channel closed before immediate transfer was started"
		})

		close(releaseListFiles)
		requireMessage(t, confirm, "confirm channel was not closed after immediate transfer was started")
		requireWaitGroupDone(t, &tm.baseWg)
		requireFileContents(t, transfer.LocalPath, testFileContents)

		got := tm.FlushTransfers(true)
		assert.NoError(t, got)
		client.AssertExpectations(t)
	})

	t.Run("queues deferred transfer and retrieves it on flush", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		tm, transfer, retrieveFileCalled, logBuf, client := setupMockedRetrieveFileManager()
		tm.AddTransfer(TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client)})

		requireNoMessage(t, retrieveFileCalled, func(got struct{}) string {
			return "deferred file retrieved before flushing"
		})

		got := tm.FlushTransfers(true)

		requireMessage(t, retrieveFileCalled, "deferred file retrieval not triggered by flushing")

		assert.NoError(t, got)
		requireFileContents(t, transfer.LocalPath, testFileContents)
		assert.Equal(t, []conductor.FileTransfer{transfer}, tm.completedTransfers)

		logOutput := logBuf.String()
		requireLogContains(t, logOutput, "[TransferManager] Listening for transfer requests")
		requireLogContains(t, logOutput, "[TransferManager] Deferred transfer enqueued: '"+transfer.RemotePath+"' -> '"+transfer.LocalPath+"'")
		requireLogContains(t, logOutput, "[TransferManager] Flushing 1 deferred transfers")
		requireLogContains(t, logOutput, "[TransferManager] Listening complete")
		client.AssertExpectations(t)
	})

	t.Run("dispatches immediate transfer before flush", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		tm, transfer, retrieveFileCalled, logBuf, client := setupMockedRetrieveFileManager()
		tm.AddTransfer(TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client), ImmediateRetrieval: true})

		requireMessage(t, retrieveFileCalled, "immediate file retrieval not executed within timeout")

		requireWaitGroupDone(t, &tm.baseWg)
		requireFileContents(t, transfer.LocalPath, testFileContents)

		got := tm.FlushTransfers(true)

		assert.NoError(t, got)
		assert.Equal(t, []conductor.FileTransfer{transfer}, tm.completedTransfers)
		logOutput := logBuf.String()
		requireLogContains(t, logOutput, "[TransferManager] Immediate transfer dispatched: '"+transfer.RemotePath+"' -> '"+transfer.LocalPath+"'")
		requireLogContains(t, logOutput, "[TransferManager] Transfer started: '"+transfer.RemotePath+"' -> '"+transfer.LocalPath+"'")
		requireLogContains(t, logOutput, "[TransferManager] Transfer succeeded: '"+transfer.RemotePath+"' -> '"+transfer.LocalPath+"'")
		client.AssertExpectations(t)
	})

	t.Run("flushes deferred phase1 transfers before deferred background transfers", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _ := newListenTestManager(client, 1)
		phase1Transfer := fileTransfer(t, false)
		phase1Transfer.RemotePath = "/remote/phase1.txt"
		backgroundTransfer := fileTransfer(t, false)
		backgroundTransfer.RemotePath = "/remote/background.txt"
		mockSingleListFiles(client, phase1Transfer.RemotePath)
		mockSingleListFiles(client, backgroundTransfer.RemotePath)
		phase1Called, releasePhase1 := mockBlockingRetrieveFile(client, phase1Transfer.RemotePath, true)
		backgroundCalled, releaseBackground := mockBlockingRetrieveFile(client, backgroundTransfer.RemotePath, true)

		go tm.Listen(tm.observer.Logger, nil, &NullStageNotifier{})
		<-tm.ListeningStarted
		tm.AddTransfer(TransferRequest{FileTransfer: backgroundTransfer, AgentSupplier: agentSupplier(client), BackgroundTransfer: true})
		tm.AddTransfer(TransferRequest{FileTransfer: phase1Transfer, AgentSupplier: agentSupplier(client)})

		resultCh := make(chan message.Message, 1)
		go func() {
			resultCh <- tm.FlushTransfers(true)
		}()

		requireMessage(t, phase1Called, "phase1 transfer was not started")
		requireNoMessage(t, backgroundCalled, func(got struct{}) string {
			return "background transfer started before phase1 transfer completed"
		})

		close(releasePhase1)
		requireMessage(t, backgroundCalled, "background transfer did not start after phase1 transfer completed")
		close(releaseBackground)

		got := waitForFlushResult(t, resultCh)
		assert.NoError(t, got)
		client.AssertExpectations(t)
	})

	t.Run("does not start deferred background transfers when cancelled after phase1 starts", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _ := newListenTestManager(client, 1)
		cancelChan := make(chan struct{})
		cmdStateChannel := &cmdsync.CommandStateChannel{
			CancelChan: cancelChan,
		}
		phase1Transfer := fileTransfer(t, false)
		phase1Transfer.RemotePath = "/remote/phase1.txt"
		backgroundTransfer := fileTransfer(t, false)
		backgroundTransfer.RemotePath = "/remote/background.txt"
		mockSingleListFiles(client, phase1Transfer.RemotePath)
		phase1Called, releasePhase1 := mockBlockingRetrieveFile(client, phase1Transfer.RemotePath, false)

		go tm.Listen(tm.observer.Logger, cmdStateChannel, &NullStageNotifier{})
		<-tm.ListeningStarted
		tm.AddTransfer(TransferRequest{FileTransfer: phase1Transfer, AgentSupplier: agentSupplier(client)})
		tm.AddTransfer(TransferRequest{FileTransfer: backgroundTransfer, AgentSupplier: agentSupplier(client), BackgroundTransfer: true})

		resultCh := make(chan message.Message, 1)
		go func() {
			resultCh <- tm.FlushTransfers(true)
		}()

		requireMessage(t, phase1Called, "phase1 transfer was not started")
		close(cancelChan)
		requireMessage(t, tm.baseCtx.Done(), "context not cancelled when cancellation message received")
		close(releasePhase1)

		got := waitForFlushResult(t, resultCh)
		expected := message.New(message.EngineCommonUserCanceled)
		assert.Equal(t, expected, got)
		client.AssertNotCalled(t, "ListFiles", mock.Anything, mock.MatchedBy(func(req *targetagentproto.ListFilesRequest) bool {
			return len(req.Paths) == 1 && req.Paths[0] == backgroundTransfer.RemotePath
		}))
		client.AssertNotCalled(t, "RetrieveFile", mock.Anything, &targetagentproto.FileRequest{Path: backgroundTransfer.RemotePath}, mock.Anything)
		client.AssertExpectations(t)
	})

	t.Run("immediate background transfer continues while deferred phase1 transfers flush", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _ := newListenTestManager(client, 2)
		immediateBackgroundTransfer := fileTransfer(t, false)
		immediateBackgroundTransfer.RemotePath = "/remote/immediate-background.txt"
		phase1Transfer := fileTransfer(t, false)
		phase1Transfer.RemotePath = "/remote/phase1.txt"
		mockSingleListFiles(client, immediateBackgroundTransfer.RemotePath)
		mockSingleListFiles(client, phase1Transfer.RemotePath)
		immediateBackgroundCalled, releaseImmediateBackground := mockBlockingRetrieveFile(client, immediateBackgroundTransfer.RemotePath, true)
		phase1Called, releasePhase1 := mockBlockingRetrieveFile(client, phase1Transfer.RemotePath, true)

		go tm.Listen(tm.observer.Logger, nil, &NullStageNotifier{})
		<-tm.ListeningStarted
		tm.AddTransfer(TransferRequest{FileTransfer: immediateBackgroundTransfer, AgentSupplier: agentSupplier(client), ImmediateRetrieval: true, BackgroundTransfer: true})
		requireMessage(t, immediateBackgroundCalled, "immediate background transfer did not start before flush")

		tm.AddTransfer(TransferRequest{FileTransfer: phase1Transfer, AgentSupplier: agentSupplier(client)})
		resultCh := make(chan message.Message, 1)
		go func() {
			resultCh <- tm.FlushTransfers(true)
		}()

		requireMessage(t, phase1Called, "phase1 transfer was not started while immediate background transfer was in progress")
		close(releasePhase1)
		requireNoMessage(t, resultCh, func(got message.Message) string {
			return "FlushTransfers returned before immediate background transfer completed"
		})

		close(releaseImmediateBackground)
		got := waitForFlushResult(t, resultCh)
		assert.NoError(t, got)
		client.AssertExpectations(t)
	})

	t.Run("waiting immediate background transfer is deprioritized during phase1 flush", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _ := newListenTestManager(client, 1)
		// Occupy the only transfer slot so the immediate background transfer is
		// resolved and waiting to start before the phase1 flush begins.
		assert.NoError(t, tm.transferSlots.Acquire(context.Background(), phase1TransferPhase))

		backgroundTransfer := fileTransfer(t, false)
		backgroundTransfer.RemotePath = "/remote/background.txt"
		phase1Transfer := fileTransfer(t, false)
		phase1Transfer.RemotePath = "/remote/phase1.txt"

		backgroundListed := make(chan struct{}, 1)
		client.On(
			"ListFiles",
			mock.Anything,
			mock.MatchedBy(func(req *targetagentproto.ListFilesRequest) bool {
				return len(req.Paths) == 1 && req.Paths[0] == backgroundTransfer.RemotePath
			}),
		).Run(func(mock.Arguments) {
			backgroundListed <- struct{}{}
		}).Return(listFilesResponse(&targetagentproto.FileInfo{
			Path: backgroundTransfer.RemotePath,
			Size: int64(len(testFileContents)),
		}), nil).Once()
		mockSingleListFiles(client, phase1Transfer.RemotePath)
		backgroundCalled, releaseBackground := mockBlockingRetrieveFile(client, backgroundTransfer.RemotePath, true)
		phase1Called, releasePhase1 := mockBlockingRetrieveFile(client, phase1Transfer.RemotePath, true)

		go tm.Listen(tm.observer.Logger, nil, &NullStageNotifier{})
		<-tm.ListeningStarted
		tm.AddTransfer(TransferRequest{FileTransfer: backgroundTransfer, AgentSupplier: agentSupplier(client), ImmediateRetrieval: true, BackgroundTransfer: true})
		requireMessage(t, backgroundListed, "background transfer was not resolved")
		tm.AddTransfer(TransferRequest{FileTransfer: phase1Transfer, AgentSupplier: agentSupplier(client)})

		resultCh := make(chan message.Message, 1)
		go func() {
			resultCh <- tm.FlushTransfers(true)
		}()

		assert.Eventually(t, func() bool {
			tm.transferSlots.mu.Lock()
			defer tm.transferSlots.mu.Unlock()
			return len(tm.transferSlots.phase1Queue) == 1
		}, time.Second, time.Millisecond)

		assert.NoError(t, tm.transferSlots.Release())
		requireMessage(t, phase1Called, "phase1 transfer did not get priority")
		requireNoMessage(t, backgroundCalled, func(got struct{}) string {
			return "background transfer started before phase1 transfer completed"
		})

		close(releasePhase1)
		requireMessage(t, backgroundCalled, "background transfer did not resume after phase1")
		close(releaseBackground)

		got := waitForFlushResult(t, resultCh)
		assert.NoError(t, got)
		client.AssertExpectations(t)
	})

	t.Run("reports phase1 progress before deferred background transfers are listed", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _ := newListenTestManager(client, 1)
		events := make(chan string, 10)
		notifier := &progressRecordingNotifier{progress: make(chan notifiers.StageProgress, 10), events: events}
		phase1Transfer := fileTransfer(t, false)
		phase1Transfer.RemotePath = "/remote/phase1.txt"
		backgroundTransfer := fileTransfer(t, false)
		backgroundTransfer.RemotePath = "/remote/background.txt"

		mockSingleListFiles(client, phase1Transfer.RemotePath)
		mockRetrieveFile(client, phase1Transfer.RemotePath)
		client.On(
			"ListFiles",
			mock.Anything,
			mock.MatchedBy(func(req *targetagentproto.ListFilesRequest) bool {
				return len(req.Paths) == 1 && req.Paths[0] == backgroundTransfer.RemotePath
			}),
		).Run(func(mock.Arguments) {
			events <- "background-listed"
		}).Return(listFilesResponse(&targetagentproto.FileInfo{
			Path: backgroundTransfer.RemotePath,
			Size: int64(len(testFileContents)),
		}), nil).Once()
		mockRetrieveFile(client, backgroundTransfer.RemotePath)

		go tm.Listen(tm.observer.Logger, nil, notifier)
		<-tm.ListeningStarted
		tm.AddTransfer(TransferRequest{FileTransfer: phase1Transfer, AgentSupplier: agentSupplier(client)})
		tm.AddTransfer(TransferRequest{FileTransfer: backgroundTransfer, AgentSupplier: agentSupplier(client), BackgroundTransfer: true})

		resultCh := make(chan message.Message, 1)
		go func() {
			resultCh <- tm.FlushTransfers(true)
		}()

		progress := requireMessage(t, notifier.progress, "phase1 progress was not reported")
		assert.Equal(t, int64(len(testFileContents)), progress.Sent)
		assert.Equal(t, int64(len(testFileContents)), progress.Max)
		assert.Empty(t, progress.Message)
		assert.Equal(t, "progress:Retrieving recipe artifacts (required)", requireMessage(t, events, "expected phase1 progress before background listing"))
		assert.Equal(t, "background-listed", requireMessage(t, events, "expected background listing after phase1 progress"))
		requireMessage(t, notifier.progress, "background progress was not reported")
		assert.Equal(t, "progress:Retrieving recipe artifacts (background)", requireMessage(t, events, "expected background progress after background listing"))

		got := waitForFlushResult(t, resultCh)
		assert.NoError(t, got)
		client.AssertExpectations(t)
	})

	t.Run("returns retrieval error when successful run has background transfer error", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _ := newListenTestManager(client, 1)
		backgroundTransfer := fileTransfer(t, false)
		backgroundTransfer.RemotePath = "/remote/background-missing.txt"
		mockListFiles(client, backgroundTransfer.RemotePath, listFilesResponse(&targetagentproto.FileInfo{
			Path:  backgroundTransfer.RemotePath,
			Error: os.ErrNotExist.Error(),
		}))

		go tm.Listen(tm.observer.Logger, nil, &NullStageNotifier{})
		<-tm.ListeningStarted
		tm.AddTransfer(TransferRequest{FileTransfer: backgroundTransfer, AgentSupplier: agentSupplier(client), BackgroundTransfer: true})

		got := tm.FlushTransfers(true)

		expected := message.New(message.EngineRecipeStagesRetrieveAgentFilesRetrieveFile)
		assert.ErrorIs(t, got, expected)
		assert.Equal(t, backgroundTransfer.RemotePath, got.Metadata()["filePath"])
		assert.NoError(t, message.ValidateMetadataPlaceholders(got))
		client.AssertExpectations(t)
	})

	t.Run("returns immediately when listen is already running", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		tm, transfer, retrieveFileCalled, _, client := setupMockedRetrieveFileManager()

		// Call Listen again and verify that it returns immediately
		secondListenReturned := make(chan struct{}, 1)
		go func() {
			// Use new observer to avoid race
			tm.Listen(logrus.New(), nil, &NullStageNotifier{})
			secondListenReturned <- struct{}{}
		}()

		requireMessage(t, secondListenReturned, "second Listen call did not return")

		// Verify that TransferManager functionality is unaffected
		tm.AddTransfer(TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client)})
		got := tm.FlushTransfers(true)

		requireMessage(t, retrieveFileCalled, "deferred file retrieval not triggered by flushing")
		assert.NoError(t, got)
		requireFileContents(t, transfer.LocalPath, testFileContents)
		assert.Equal(t, []conductor.FileTransfer{transfer}, tm.completedTransfers)
		client.AssertExpectations(t)
	})

	t.Run("cancels immediately when cancel channel is already closed", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _ := newListenTestManager(client, 1)
		cancelChan := make(chan struct{})
		close(cancelChan)
		cmdStateChannel := &cmdsync.CommandStateChannel{
			CancelChan: cancelChan,
		}

		go tm.Listen(tm.observer.Logger, cmdStateChannel, &enginemocks.MockStageNotifier{})

		requireMessage(t, tm.ListeningStarted, "Listen did not start")
		requireMessage(t, tm.listeningDone, "listeningDone was not closed")

		requireFlushNoLongerListening(t, tm, true)
		client.AssertExpectations(t)
	})

	t.Run("returns cancellation message when cancel channel is signaled while flushing", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _ := newListenTestManager(client, 1)
		cancelChan := make(chan struct{})
		cmdStateChannel := &cmdsync.CommandStateChannel{
			CancelChan: cancelChan,
		}
		transfer := fileTransfer(t, false)
		mockSingleListFiles(client, transfer.RemotePath)

		retrieveFileCalled := mockContextCancelledRetrieveFile(client, transfer.RemotePath)

		go tm.Listen(tm.observer.Logger, cmdStateChannel, &NullStageNotifier{})
		<-tm.ListeningStarted
		tm.AddTransfer(TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client)})

		resultCh := make(chan message.Message, 1)
		go func() {
			resultCh <- tm.FlushTransfers(true)
		}()

		requireMessage(t, retrieveFileCalled, "RetrieveFile was not called")
		close(cancelChan)
		got := waitForFlushResult(t, resultCh)

		expected := message.New(message.EngineCommonUserCanceled)
		assert.Equal(t, expected, got)
		assert.NoError(t, message.ValidateMetadataPlaceholders(got))
		client.AssertExpectations(t)
	})

	t.Run("cancels in-flight transfer when cancel channel is signaled while listening", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _ := newListenTestManager(client, 1)
		cancelChan := make(chan struct{})
		cmdStateChannel := &cmdsync.CommandStateChannel{
			CancelChan: cancelChan,
		}
		transfer := fileTransfer(t, false)
		mockSingleListFiles(client, transfer.RemotePath)

		retrieveFileCalled := mockContextCancelledRetrieveFile(client, transfer.RemotePath)

		go tm.Listen(tm.observer.Logger, cmdStateChannel, &NullStageNotifier{})
		<-tm.ListeningStarted
		tm.AddTransfer(TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client), ImmediateRetrieval: true})

		requireMessage(t, retrieveFileCalled, "RetrieveFile was not called")

		close(cancelChan)

		requireMessage(t, tm.baseCtx.Done(), "base context not cancelled")
		requireMessage(t, tm.mandatoryCtx.Done(), "mandatory context not cancelled")
		requireWaitGroupDone(t, &tm.baseWg)

		// Assert that the in-flight transfer was not completed
		assert.Empty(t, tm.getCompletedTransfersCopy())
		client.AssertExpectations(t)
	})

	t.Run("closes listeningDone when cancel channel is signaled so new requests return immediately", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _ := newListenTestManager(client, 1)
		cancelChan := make(chan struct{})
		cmdStateChannel := &cmdsync.CommandStateChannel{
			CancelChan: cancelChan,
		}
		transfer := fileTransfer(t, false)
		mockSingleListFiles(client, transfer.RemotePath)

		retrieveFileCalled, releaseTransfer := mockBlockingRetrieveFile(client, transfer.RemotePath, false)

		listenReturned := make(chan struct{})
		go func() {
			tm.Listen(tm.observer.Logger, cmdStateChannel, &NullStageNotifier{})
			close(listenReturned)
		}()
		<-tm.ListeningStarted
		tm.AddTransfer(TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client), ImmediateRetrieval: true})
		requireMessage(t, retrieveFileCalled, "RetrieveFile was not called")

		close(cancelChan)
		requireMessage(t, tm.listeningDone, "listeningDone was not closed")

		addDone := make(chan struct{}, 1)
		go func() {
			newTransfer := fileTransfer(t, false)
			newTransfer.RemotePath = "/remote/new-output.txt"
			tm.AddTransfer(TransferRequest{FileTransfer: newTransfer, AgentSupplier: agentSupplier(client), ImmediateRetrieval: true})
			addDone <- struct{}{}
		}()
		requireMessage(t, addDone, "AddTransfer did not return after cancellation")

		requireFlushNoLongerListening(t, tm, true)

		close(releaseTransfer)
		requireWaitGroupDone(t, &tm.baseWg)
		requireMessage(t, listenReturned, "Listen did not return after transfer was released")
		client.AssertExpectations(t)
	})

	t.Run("closes listeningDone when flush request received so new requests return immediately", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _ := newListenTestManager(client, 1)
		transfer := fileTransfer(t, false)
		mockSingleListFiles(client, transfer.RemotePath)

		retrieveFileCalled, releaseTransfer := mockBlockingRetrieveFile(client, transfer.RemotePath, true)

		go tm.Listen(tm.observer.Logger, nil, &NullStageNotifier{})
		<-tm.ListeningStarted
		tm.AddTransfer(TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client)})

		resultCh := make(chan message.Message, 1)
		go func() {
			resultCh <- tm.FlushTransfers(true)
		}()
		requireMessage(t, retrieveFileCalled, "RetrieveFile was not called")

		addDone := make(chan struct{}, 1)
		go func() {
			newTransfer := fileTransfer(t, false)
			newTransfer.RemotePath = "/remote/new-output.txt"
			tm.AddTransfer(TransferRequest{FileTransfer: newTransfer, AgentSupplier: agentSupplier(client)})
			addDone <- struct{}{}
		}()
		requireMessage(t, addDone, "AddTransfer did not return while flush was waiting")

		close(releaseTransfer)
		got := waitForFlushResult(t, resultCh)

		assert.NoError(t, got)
		assert.Equal(t, []conductor.FileTransfer{transfer}, tm.completedTransfers)
		requireFileContents(t, transfer.LocalPath, testFileContents)
		client.AssertExpectations(t)
	})
}

func TestTransferManagerFlushTransfers(t *testing.T) {
	waitForFlushResult := func(t *testing.T, resultCh <-chan message.Message) message.Message {
		return requireMessage(t, resultCh, "flushTransfers did not return")
	}
	mockBlockingRetrieveFile := func(
		client *targetagentmocks.TargetAgentClient,
		remotePath string,
		finishSuccessfully bool,
	) (chan struct{}, chan struct{}) {
		retrieveFileCalled := make(chan struct{}, 1)
		releaseTransfer := make(chan struct{})
		stream := &agentmocks.MockRetrieveFileStream{}
		recvErr := error(nil)
		if !finishSuccessfully {
			recvErr = context.Canceled
		}
		stream.On("RecvMsg", mock.Anything).Run(func(args mock.Arguments) {
			retrieveFileCalled <- struct{}{}
			<-releaseTransfer
			if finishSuccessfully {
				args.Get(0).(*targetagentproto.FileContent).Content = []byte(testFileContents)
			}
		}).Return(recvErr).Once()
		if finishSuccessfully {
			stream.On("RecvMsg", mock.Anything).Return(io.EOF).Once()
		}

		client.On(
			"RetrieveFile",
			mock.Anything,
			&targetagentproto.FileRequest{Path: remotePath},
			mock.Anything,
		).Return(stream, nil).Once()

		return retrieveFileCalled, releaseTransfer
	}

	t.Run("transfers deferred files and returns nil when run succeeded", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, logBuf, baseCancel, _ := newStartTransferTestManager(t, client, 1)

		transfer := fileTransfer(t, false)
		tm.deferredPhase1Transfers = []TransferRequest{{FileTransfer: transfer, AgentSupplier: agentSupplier(client)}}
		mockSingleListFiles(client, transfer.RemotePath)
		mockRetrieveFile(client, transfer.RemotePath)

		got := tm.flushTransfers(FlushTransfersMessage{runSucceeded: true}, baseCancel)

		assert.NoError(t, got)
		requireFileContents(t, transfer.LocalPath, testFileContents)
		assert.Equal(t, []conductor.FileTransfer{transfer}, tm.completedTransfers)
		requireNoTransferErrors(t, tm)
		requireLogContains(t, logBuf.String(), "[TransferManager] Flushing 1 deferred transfers")
		client.AssertExpectations(t)
	})

	t.Run("notifies phase1 completion before background transfers finish", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, baseCancel, _ := newStartTransferTestManager(t, client, 1)

		phase1Transfer := fileTransfer(t, false)
		phase1Transfer.RemotePath = "/remote/phase1.txt"
		backgroundTransfer := fileTransfer(t, false)
		backgroundTransfer.RemotePath = "/remote/background.txt"
		tm.deferredPhase1Transfers = []TransferRequest{{FileTransfer: phase1Transfer, AgentSupplier: agentSupplier(client)}}
		tm.deferredBackgroundTransfers = []TransferRequest{{FileTransfer: backgroundTransfer, AgentSupplier: agentSupplier(client), BackgroundTransfer: true}}
		tm.backgroundTransferCount = 1

		mockSingleListFiles(client, phase1Transfer.RemotePath)
		mockRetrieveFile(client, phase1Transfer.RemotePath)
		mockSingleListFiles(client, backgroundTransfer.RemotePath)
		backgroundCalled, releaseBackground := mockBlockingRetrieveFile(client, backgroundTransfer.RemotePath, true)

		phase1CompleteBeforeBackground := make(chan bool, 1)
		hasPendingBackgroundTransfersAtPhase1 := make(chan bool, 1)
		tm.OnPhase1TransferComplete = func(hasPendingBackgroundTransfers bool) {
			hasPendingBackgroundTransfersAtPhase1 <- hasPendingBackgroundTransfers
			select {
			case <-backgroundCalled:
				phase1CompleteBeforeBackground <- false
			default:
				phase1CompleteBeforeBackground <- true
			}
		}
		resultCh := make(chan message.Message, 1)
		go func() {
			resultCh <- tm.flushTransfers(FlushTransfersMessage{runSucceeded: true}, baseCancel)
		}()

		assert.True(t, requireMessage(t, phase1CompleteBeforeBackground, "phase1 completion was not reported before background transfer started"))
		assert.True(t, requireMessage(t, hasPendingBackgroundTransfersAtPhase1, "background work was not reported at phase1 completion"))
		requireMessage(t, backgroundCalled, "background transfer did not start")
		requireNoMessage(t, resultCh, func(got message.Message) string {
			return "flushTransfers returned before background transfer finished"
		})
		close(releaseBackground)

		got := waitForFlushResult(t, resultCh)
		assert.NoError(t, got)
		client.AssertExpectations(t)
	})

	t.Run("reports no pending background transfers when immediate background finishes before phase1", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, baseCancel, _ := newStartTransferTestManager(t, client, 1)

		backgroundTransfer := fileTransfer(t, false)
		backgroundTransfer.RemotePath = "/remote/background.txt"
		mockSingleListFiles(client, backgroundTransfer.RemotePath)
		mockRetrieveFile(client, backgroundTransfer.RemotePath)

		tm.addTransferCount(backgroundTransferPhase)
		tm.startTransferRequest(TransferRequest{
			FileTransfer:       backgroundTransfer,
			AgentSupplier:      agentSupplier(client),
			ImmediateRetrieval: true,
			BackgroundTransfer: true,
		})
		tm.backgroundWg.Wait()

		phase1Transfer := fileTransfer(t, false)
		phase1Transfer.RemotePath = "/remote/phase1.txt"
		mockSingleListFiles(client, phase1Transfer.RemotePath)
		mockRetrieveFile(client, phase1Transfer.RemotePath)
		tm.deferredPhase1Transfers = []TransferRequest{{
			FileTransfer:  phase1Transfer,
			AgentSupplier: agentSupplier(client),
		}}

		phase1CallbackCalled := false
		pendingAtPhase1 := true
		tm.OnPhase1TransferComplete = func(hasPendingBackgroundTransfers bool) {
			phase1CallbackCalled = true
			pendingAtPhase1 = hasPendingBackgroundTransfers
		}

		got := tm.flushTransfers(FlushTransfersMessage{runSucceeded: true}, baseCancel)

		assert.NoError(t, got)
		require.True(t, phase1CallbackCalled, "phase1 callback was not invoked")
		assert.False(t, pendingAtPhase1)
		client.AssertExpectations(t)
	})

	t.Run("does not notify phase1 completion when phase1 transfer fails", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, baseCancel, _ := newStartTransferTestManager(t, client, 1)

		transfer := fileTransfer(t, false)
		tm.deferredPhase1Transfers = []TransferRequest{{FileTransfer: transfer, AgentSupplier: agentSupplier(client)}}
		mockSingleListFiles(client, transfer.RemotePath)
		_, releaseTransfer := mockBlockingRetrieveFile(client, transfer.RemotePath, false)

		phase1Complete := make(chan struct{}, 1)
		tm.OnPhase1TransferComplete = func(bool) {
			phase1Complete <- struct{}{}
		}
		resultCh := make(chan message.Message, 1)
		go func() {
			resultCh <- tm.flushTransfers(FlushTransfersMessage{runSucceeded: true}, baseCancel)
		}()
		close(releaseTransfer)

		got := waitForFlushResult(t, resultCh)
		assert.Error(t, got)
		requireNoMessage(t, phase1Complete, func(got struct{}) string {
			return "phase1 completion was reported after phase1 transfer failed"
		})
		client.AssertExpectations(t)
	})

	t.Run("reports required phase lifecycle even when there are no transfers", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, baseCancel, _ := newStartTransferTestManager(t, client, 1)
		notifier := &stageLifecycleRecordingNotifier{}
		tm.notifier = notifier

		got := tm.flushTransfers(FlushTransfersMessage{runSucceeded: true}, baseCancel)

		assert.NoError(t, got)
		assert.Equal(t, []string{
			"start:Retrieving recipe artifacts (required)",
			"end:Retrieving recipe artifacts (required)",
		}, notifier.events)
		client.AssertExpectations(t)
	})

	t.Run("keeps phase1 stage active until phase1 completion callback returns", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, baseCancel, _ := newStartTransferTestManager(t, client, 1)
		notifier := &stageLifecycleRecordingNotifier{}
		tm.notifier = notifier
		tm.OnPhase1TransferComplete = func(hasPendingBackgroundTransfers bool) {
			assert.False(t, hasPendingBackgroundTransfers)
			// Assert that the stage has not ended yet
			// This ensures that when the stage does end, the run result will already have been updated
			// in the manifest and the run is therefore loadable in the GUI
			assert.Equal(t, []string{
				"start:Retrieving recipe artifacts (required)",
			}, notifier.events)
		}

		got := tm.flushTransfers(FlushTransfersMessage{runSucceeded: true}, baseCancel)

		assert.NoError(t, got)
		assert.Equal(t, []string{
			"start:Retrieving recipe artifacts (required)",
			"end:Retrieving recipe artifacts (required)",
		}, notifier.events)
		client.AssertExpectations(t)
	})

	t.Run("does not report background phase lifecycle when there are no background transfers", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, baseCancel, _ := newStartTransferTestManager(t, client, 1)
		notifier := &stageLifecycleRecordingNotifier{}
		tm.notifier = notifier

		transfer := fileTransfer(t, false)
		tm.deferredPhase1Transfers = []TransferRequest{{FileTransfer: transfer, AgentSupplier: agentSupplier(client)}}
		mockSingleListFiles(client, transfer.RemotePath)
		mockRetrieveFile(client, transfer.RemotePath)

		got := tm.flushTransfers(FlushTransfersMessage{runSucceeded: true}, baseCancel)

		assert.NoError(t, got)
		assert.Equal(t, []string{
			"start:Retrieving recipe artifacts (required)",
			"end:Retrieving recipe artifacts (required)",
		}, notifier.events)
		client.AssertExpectations(t)
	})

	t.Run("does not report duplicate background start when phase already started", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, baseCancel, _ := newStartTransferTestManager(t, client, 1)
		notifier := &stageLifecycleRecordingNotifier{}
		tm.notifier = notifier

		transfer := fileTransfer(t, false)
		tm.backgroundTransferCount = 1
		backgroundReq := TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client), BackgroundTransfer: true}
		tm.deferredBackgroundTransfers = []TransferRequest{backgroundReq}
		mockSingleListFiles(client, transfer.RemotePath)
		mockRetrieveFile(client, transfer.RemotePath)

		tm.startPhaseStage(backgroundTransferPhase)
		got := tm.flushTransfers(FlushTransfersMessage{runSucceeded: true}, baseCancel)

		assert.NoError(t, got)
		assert.Equal(t, []string{
			"start:Retrieving recipe artifacts (background)",
			"start:Retrieving recipe artifacts (required)",
			"end:Retrieving recipe artifacts (required)",
			"end:Retrieving recipe artifacts (background)",
		}, notifier.events)
		client.AssertExpectations(t)
	})

	t.Run("cancels in-progress optional transfer but waits for in-progress mandatory transfer when run failed", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, baseCancel, _ := newStartTransferTestManager(t, client, 2)

		optionalTransfer := fileTransfer(t, false)
		// Override optional remote path to avoid conflicts
		optionalTransfer.RemotePath = "/optionalPath.txt"
		mandatoryTransfer := fileTransfer(t, true)
		mockSingleListFiles(client, optionalTransfer.RemotePath)
		mockSingleListFiles(client, mandatoryTransfer.RemotePath)

		inStream := make(chan struct{}, 2)

		optionalRetrieveCtx := make(chan context.Context, 2)
		optionalStream := &agentmocks.MockRetrieveFileStream{}
		optionalStream.On("RecvMsg", mock.Anything).Run(func(args mock.Arguments) {
			inStream <- struct{}{}
			ctx := <-optionalRetrieveCtx
			<-ctx.Done()
		}).Return(errors.New("optional transfer stopped")).Once()
		client.On("RetrieveFile", mock.Anything, &targetagentproto.FileRequest{Path: optionalTransfer.RemotePath}, mock.Anything).Run(func(args mock.Arguments) {
			optionalRetrieveCtx <- args.Get(0).(context.Context)
		}).Return(optionalStream, nil).Once()

		releaseMandatoryTransfer := make(chan struct{})
		mandatoryStream := &agentmocks.MockRetrieveFileStream{}
		mandatoryStream.On("RecvMsg", mock.Anything).Run(func(args mock.Arguments) {
			inStream <- struct{}{}
			<-releaseMandatoryTransfer
			args.Get(0).(*targetagentproto.FileContent).Content = []byte(testFileContents)
		}).Return(nil).Once()
		mandatoryStream.On("RecvMsg", mock.Anything).Return(io.EOF).Once()
		client.On("RetrieveFile", mock.Anything, &targetagentproto.FileRequest{Path: mandatoryTransfer.RemotePath}, mock.Anything).Return(mandatoryStream, nil).Once()

		tm.startTransferRequest(TransferRequest{FileTransfer: optionalTransfer, AgentSupplier: agentSupplier(client)})
		tm.startTransferRequest(TransferRequest{FileTransfer: mandatoryTransfer, AgentSupplier: agentSupplier(client)})

		for i := 0; i < 2; i++ {
			requireMessage(t, inStream, "transfers were not started in time")
		}

		resultCh := make(chan message.Message, 1)
		go func() {
			resultCh <- tm.flushTransfers(FlushTransfersMessage{runSucceeded: false}, baseCancel)
		}()

		requireMessage(t, tm.baseCtx.Done(), "optional transfer context was not cancelled")

		requireNoMessage(t, tm.mandatoryCtx.Done(), func(got struct{}) string {
			return "mandatory transfer context was cancelled"
		})

		close(releaseMandatoryTransfer)
		got := waitForFlushResult(t, resultCh)

		assert.NoError(t, got)
		assert.Equal(t, []conductor.FileTransfer{mandatoryTransfer}, tm.completedTransfers)
		requireFileContents(t, mandatoryTransfer.LocalPath, testFileContents)
		client.AssertExpectations(t)
	})

	t.Run("skips optional deferred transfers but retrieves mandatory deferred transfers when run failed", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, baseCancel, _ := newStartTransferTestManager(t, client, 1)

		optionalTransfer := fileTransfer(t, false)
		mandatoryTransfer := fileTransfer(t, true)
		tm.deferredPhase1Transfers = []TransferRequest{
			{FileTransfer: optionalTransfer, AgentSupplier: agentSupplier(client)},
			{FileTransfer: mandatoryTransfer, AgentSupplier: agentSupplier(client)},
		}
		mockSingleListFiles(client, mandatoryTransfer.RemotePath)
		mockRetrieveFile(client, mandatoryTransfer.RemotePath)

		got := tm.flushTransfers(FlushTransfersMessage{runSucceeded: false}, baseCancel)

		assert.NoError(t, got)
		_, err := os.Stat(optionalTransfer.LocalPath)
		assert.True(t, os.IsNotExist(err), "expected optional deferred transfer to be skipped")
		requireFileContents(t, mandatoryTransfer.LocalPath, testFileContents)
		assert.Equal(t, []conductor.FileTransfer{mandatoryTransfer}, tm.completedTransfers)
		requireNoTransferErrors(t, tm)
		client.AssertExpectations(t)
	})

	t.Run("rolls back optional completed transfer but keeps mandatory completed transfer when run failed", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, baseCancel, _ := newStartTransferTestManager(t, client, 1)

		optionalTransfer := fileTransfer(t, false)
		mandatoryTransfer := fileTransfer(t, true)
		assert.NoError(t, os.WriteFile(optionalTransfer.LocalPath, []byte("retrieved data"), 0o600))
		assert.NoError(t, os.WriteFile(mandatoryTransfer.LocalPath, []byte("log"), 0o600))
		tm.completedTransfers = []conductor.FileTransfer{optionalTransfer, mandatoryTransfer}

		got := tm.flushTransfers(FlushTransfersMessage{runSucceeded: false}, baseCancel)

		assert.NoError(t, got)
		_, err := os.Stat(optionalTransfer.LocalPath)
		assert.True(t, os.IsNotExist(err), "expected optional completed transfer to be rolled back")
		requireFileContents(t, mandatoryTransfer.LocalPath, "log")
		requireNoTransferErrors(t, tm)
		client.AssertExpectations(t)
	})

	t.Run("returns single-file retrieval message when successful run has one retrieval error", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, baseCancel, _ := newStartTransferTestManager(t, client, 1)
		transfer := fileTransfer(t, false)
		transferErr := message.New(message.EnginePathRemapWildcardSuffixMismatch).WithMetadata(map[string]string{
			"localPath":  "output/result.csv",
			"remoteBase": "tmp/out/*.csv",
		})
		tm.transferErrors.add(phase1TransferPhase, transfer, transferErr)

		got := tm.flushTransfers(FlushTransfersMessage{runSucceeded: true}, baseCancel)

		expected := message.New(message.EngineRecipeStagesRetrieveAgentFilesRetrieveFile).WithMetadata(map[string]string{
			"filePath": transfer.RemotePath,
			"error":    transferErr.Error(),
		})
		assert.ErrorIs(t, got, expected)
		assert.ErrorIs(t, got, transferErr)
		assert.NoError(t, message.ValidateMetadataPlaceholders(got))
		client.AssertExpectations(t)
	})

	t.Run("returns multi-file retrieval message when successful run has multiple retrieval errors", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, baseCancel, _ := newStartTransferTestManager(t, client, 1)
		firstTransfer := fileTransfer(t, false)
		firstTransfer.RemotePath = "/remote/first.txt"
		secondTransfer := fileTransfer(t, false)
		secondTransfer.RemotePath = "/remote/second.txt"
		firstErr := errors.New("first failed")
		secondErr := errors.New("second failed")
		tm.transferErrors.add(phase1TransferPhase, firstTransfer, firstErr)
		tm.transferErrors.add(backgroundTransferPhase, secondTransfer, secondErr)

		got := tm.flushTransfers(FlushTransfersMessage{runSucceeded: true}, baseCancel)

		expected := message.New(message.EngineRecipeStagesRetrieveAgentFilesRetrieveFiles).WithMetadata(map[string]string{
			transferErrorPath(firstTransfer):  firstErr.Error(),
			transferErrorPath(secondTransfer): secondErr.Error(),
			"numFiles":                        "2",
		})
		assert.ErrorIs(t, got, expected)
		assert.ErrorIs(t, got, firstErr)
		assert.ErrorIs(t, got, secondErr)
		assert.NoError(t, message.ValidateMetadataPlaceholders(got))
		client.AssertExpectations(t)
	})

	t.Run("ignores retrieval errors when run already failed", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, baseCancel, _ := newStartTransferTestManager(t, client, 1)

		got := tm.flushTransfers(FlushTransfersMessage{runSucceeded: false}, baseCancel)
		assert.NoError(t, got)
		client.AssertExpectations(t)
	})

	t.Run("returns early if transfer manager hasn't started listening", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		tm := NewTransferManager(1, nil)

		resultCh := make(chan message.Message, 1)
		go func() {
			resultCh <- tm.FlushTransfers(true)
		}()

		got := waitForFlushResult(t, resultCh)

		expected := message.New(message.CommonUnknownError)
		assert.ErrorIs(t, got, expected)
		assert.Contains(t, got.Error(), "TransferManager has not started listening yet")
	})

	t.Run("returns early if transfer manager has already been flushed / cancelled", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		tm := NewTransferManager(1, nil)
		close(tm.ListeningStarted)
		close(tm.listeningDone)

		resultCh := make(chan message.Message, 1)
		go func() {
			resultCh <- tm.FlushTransfers(true)
		}()

		got := waitForFlushResult(t, resultCh)

		expected := message.New(message.CommonUnknownError)
		assert.ErrorIs(t, got, expected)
		assert.Contains(t, got.Error(), "TransferManager is no longer listening")
	})
}

func TestTransferManagerAddTransfer(t *testing.T) {
	t.Run("waits for confirm channel to close after sending transfer request", func(t *testing.T) {
		tm := NewTransferManager(1, nil)
		close(tm.ListeningStarted)
		transfer := fileTransfer(t, false)

		done := make(chan struct{}, 1)
		go func() {
			tm.AddTransfer(TransferRequest{FileTransfer: transfer})
			done <- struct{}{}
		}()

		msg := requireMessage(t, tm.transferRequestChannel, "AddTransfer did not send transfer request")
		assert.Equal(t, TransferRequest{FileTransfer: transfer}, msg.t)
		requireNoMessage(t, done, func(got struct{}) string {
			return "AddTransfer returned before confirm channel was closed"
		})

		close(msg.confirm)
		requireMessage(t, done, "AddTransfer did not return after confirm channel was closed")
	})

	t.Run("returns early if transfer manager hasn't started listening", func(t *testing.T) {
		tm := NewTransferManager(1, nil)
		transfer := fileTransfer(t, false)

		done := make(chan struct{}, 1)
		go func() {
			tm.AddTransfer(TransferRequest{FileTransfer: transfer})
			done <- struct{}{}
		}()

		requireMessage(t, done, "AddTransfer did not return")
	})

	t.Run("returns early if transfer manager has already been flushed / cancelled", func(t *testing.T) {
		tm := NewTransferManager(1, nil)
		close(tm.ListeningStarted)
		close(tm.listeningDone)
		transfer := fileTransfer(t, false)

		done := make(chan struct{}, 1)
		go func() {
			tm.AddTransfer(TransferRequest{FileTransfer: transfer})
			done <- struct{}{}
		}()

		requireMessage(t, done, "AddTransfer did not return")
	})
}

func TestTransferManagerStartTransfer(t *testing.T) {
	t.Run("retrieves file and records completed transfer", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, logBuf, _, _ := newStartTransferTestManager(t, client, 1)
		transfer := fileTransfer(t, false)
		mockSingleListFiles(client, transfer.RemotePath)
		mockRetrieveFile(client, transfer.RemotePath)

		tm.startTransferRequest(TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client)})
		requireWaitGroupDone(t, &tm.baseWg)

		requireFileContents(t, transfer.LocalPath, testFileContents)
		assert.Equal(t, []conductor.FileTransfer{transfer}, tm.completedTransfers)
		requireNoTransferErrors(t, tm)
		logOutput := logBuf.String()
		requireLogContains(t, logOutput, "[TransferManager] Transfer started: '"+transfer.RemotePath+"' -> '"+transfer.LocalPath+"'")
		requireLogContains(t, logOutput, "[TransferManager] Transfer succeeded: '"+transfer.RemotePath+"' -> '"+transfer.LocalPath+"'")
		client.AssertExpectations(t)
	})

	t.Run("uses per-transfer source agent supplier", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		targetClient := &targetagentmocks.TargetAgentClient{}
		hostClient := &targetagentmocks.TargetAgentClient{}
		tm, _, _, _ := newStartTransferTestManager(t, targetClient, 1)
		transfer := fileTransfer(t, false)
		mockSingleListFiles(hostClient, transfer.RemotePath)
		mockRetrieveFile(hostClient, transfer.RemotePath)

		tm.startTransferRequest(TransferRequest{
			FileTransfer:  transfer,
			AgentSupplier: agentSupplier(hostClient),
		})
		requireWaitGroupDone(t, &tm.baseWg)

		requireFileContents(t, transfer.LocalPath, testFileContents)
		assert.Equal(t, []conductor.FileTransfer{transfer}, tm.completedTransfers)
		requireNoTransferErrors(t, tm)
		hostClient.AssertExpectations(t)
		targetClient.AssertNotCalled(t, "ListFiles", mock.Anything, mock.Anything)
		targetClient.AssertNotCalled(t, "RetrieveFile", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("retrieves globbed files and skips directories", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, logBuf, _, _ := newStartTransferTestManager(t, client, 1)
		transfer := globFileTransfer(t)
		firstRemote := "/remote/first.txt"
		secondRemote := "/remote/second.txt"
		mockListFiles(client, transfer.RemotePath, listFilesResponse(
			&targetagentproto.FileInfo{Path: firstRemote, Size: 5},
			&targetagentproto.FileInfo{Path: "/remote/subdir", IsDir: true},
			&targetagentproto.FileInfo{Path: secondRemote, Size: 6},
		))
		mockRetrieveFileWithContents(client, firstRemote, "first")
		mockRetrieveFileWithContents(client, secondRemote, "second")

		tm.startTransferRequest(TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client)})
		requireWaitGroupDone(t, &tm.baseWg)

		firstLocal := filepath.Join(filepath.Dir(transfer.LocalPath), "first.txt")
		secondLocal := filepath.Join(filepath.Dir(transfer.LocalPath), "second.txt")
		requireFileContents(t, firstLocal, "first")
		requireFileContents(t, secondLocal, "second")
		assert.ElementsMatch(t, []conductor.FileTransfer{
			{RemotePath: firstRemote, LocalPath: firstLocal},
			{RemotePath: secondRemote, LocalPath: secondLocal},
		}, tm.completedTransfers)
		requireNoTransferErrors(t, tm)
		requireLogContains(t, logBuf.String(), "[TransferManager] Transfer skipped: '/remote/subdir' -> '"+transfer.LocalPath+"': directory skipped")
		client.AssertExpectations(t)
	})

	t.Run("skips files in exclude list", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, logBuf, _, _ := newStartTransferTestManager(t, client, 1)
		transfer := globFileTransfer(t)
		includedRemote := "/remote/included.txt"
		excludedRemote := "/remote/excluded.txt"
		transfer.Exclude = []string{"/remote/exc*.txt"}
		mockListFiles(client, transfer.RemotePath, listFilesResponse(
			&targetagentproto.FileInfo{Path: includedRemote, Size: int64(len("included"))},
			&targetagentproto.FileInfo{Path: excludedRemote, Size: int64(len("excluded"))},
		))
		mockRetrieveFileWithContents(client, includedRemote, "included")

		tm.startTransferRequest(TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client)})
		requireWaitGroupDone(t, &tm.baseWg)

		includedLocal := filepath.Join(filepath.Dir(transfer.LocalPath), "included.txt")
		excludedLocal := filepath.Join(filepath.Dir(transfer.LocalPath), "excluded.txt")
		requireFileContents(t, includedLocal, "included")
		_, err := os.Stat(excludedLocal)
		assert.True(t, os.IsNotExist(err), "expected excluded file not to be retrieved")
		assert.Equal(t, []conductor.FileTransfer{
			{RemotePath: includedRemote, LocalPath: includedLocal},
		}, tm.completedTransfers)
		requireNoTransferErrors(t, tm)
		requireLogContains(t, logBuf.String(), "[TransferManager] Transfer skipped: '"+excludedRemote+"' -> '"+transfer.LocalPath+"': remote path matches exclusion list")
		client.AssertExpectations(t)
	})

	t.Run("skips optional glob with no remote matches", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, logBuf, _, _ := newStartTransferTestManager(t, client, 1)
		transfer := globFileTransfer(t)
		mockListFiles(client, transfer.RemotePath, listFilesResponse(&targetagentproto.FileInfo{
			Error: os.ErrNotExist.Error(),
		}))

		tm.startTransferRequest(TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client)})
		requireWaitGroupDone(t, &tm.baseWg)

		assert.Empty(t, tm.completedTransfers)
		requireNoTransferErrors(t, tm)
		requireLogContains(t, logBuf.String(), "[TransferManager] Transfer skipped: '"+transfer.RemotePath+"' -> '"+transfer.LocalPath+"': glob expansion has no matches")
		client.AssertExpectations(t)
	})

	t.Run("does not skip missing concrete remote when local is globbed", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, _, _ := newStartTransferTestManager(t, client, 1)
		transfer := globFileTransfer(t)
		transfer.RemotePath = testRemotePath
		mockListFiles(client, transfer.RemotePath, listFilesResponse(&targetagentproto.FileInfo{
			Path:  transfer.RemotePath,
			Error: os.ErrNotExist.Error(),
		}))

		tm.startTransferRequest(TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client)})
		requireWaitGroupDone(t, &tm.baseWg)

		got := requireTransferError(t, tm, phase1TransferPhase, transfer)
		assert.Contains(t, got, `file does not exist on target: "file does not exist"`)
		assert.Empty(t, tm.completedTransfers)
		client.AssertExpectations(t)
	})

	t.Run("reports list files failure as unknown message", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, _, _ := newStartTransferTestManager(t, client, 1)
		transfer := fileTransfer(t, false)
		listErr := errors.New("list files failed")
		client.On("ListFiles", mock.Anything, mock.Anything).Return((*targetagentproto.ListFilesResponse)(nil), listErr).Once()

		tm.startTransferRequest(TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client)})

		got := requireTransferError(t, tm, phase1TransferPhase, transfer)
		expected := message.New(message.CommonUnknownError).WithCause(listErr)
		assert.Equal(t, expected.Error(), got)
		assert.NoError(t, message.ValidateMetadataPlaceholders(expected))
		assert.Empty(t, tm.completedTransfers)
		client.AssertExpectations(t)
	})

	t.Run("reports unexpected list files response count", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, _, _ := newStartTransferTestManager(t, client, 1)
		transfer := fileTransfer(t, false)
		client.On("ListFiles", mock.Anything, mock.Anything).Return(&targetagentproto.ListFilesResponse{}, nil).Once()

		tm.startTransferRequest(TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client)})

		got := requireTransferError(t, tm, phase1TransferPhase, transfer)
		assert.Contains(t, got, message.CommonUnknownError)
		assert.Contains(t, got, "expected 1 file info, got 0")
		assert.Empty(t, tm.completedTransfers)
		client.AssertExpectations(t)
	})

	t.Run("reports non mandatory file info error without retrieval", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, _, _ := newStartTransferTestManager(t, client, 1)
		transfer := fileTransfer(t, false)
		fileInfoErr := "permission denied"
		mockListFiles(client, transfer.RemotePath, listFilesResponse(&targetagentproto.FileInfo{
			Path:  transfer.RemotePath,
			Error: fileInfoErr,
		}))

		tm.startTransferRequest(TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client)})

		got := requireTransferError(t, tm, phase1TransferPhase, transfer)
		assert.Equal(t, "file does not exist on target: \"permission denied\"", got)
		assert.Empty(t, tm.completedTransfers)
		client.AssertExpectations(t)
	})

	t.Run("ignores mandatory file info error", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, _, _ := newStartTransferTestManager(t, client, 1)
		transfer := fileTransfer(t, true)
		mockListFiles(client, transfer.RemotePath, listFilesResponse(&targetagentproto.FileInfo{
			Path:  transfer.RemotePath,
			Error: "permission denied",
		}))

		tm.startTransferRequest(TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client)})
		requireWaitGroupDone(t, &tm.phase1Wg)

		requireNoTransferErrors(t, tm)
		assert.Empty(t, tm.completedTransfers)
		client.AssertExpectations(t)
	})

	t.Run("uses mandatory context for mandatory transfer", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, baseCancel, _ := newStartTransferTestManager(t, client, 1)
		transfer := fileTransfer(t, true)
		assert.True(t, isTransferMandatory(transfer))

		mockSingleListFiles(client, transfer.RemotePath)

		// Mock stream to block until approval is sent on releaseTransfer
		retrieveStarted := make(chan context.Context, 1)
		releaseTransfer := make(chan struct{})
		stream := &agentmocks.MockRetrieveFileStream{}
		stream.On("RecvMsg", mock.Anything).Run(func(args mock.Arguments) {
			<-releaseTransfer
			args.Get(0).(*targetagentproto.FileContent).Content = []byte("log")
		}).Return(nil).Once()
		stream.On("RecvMsg", mock.Anything).Return(io.EOF).Once()

		client.On("RetrieveFile", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			retrieveStarted <- args.Get(0).(context.Context)
		}).Return(stream, nil).Once()

		tm.startTransferRequest(TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client)})
		retrieveCtx := requireMessage(t, retrieveStarted, "RetrieveFile was not called")
		baseCancel()

		// Verify that cancelling the base context did not cancel this transfer
		requireNoMessage(t, retrieveCtx.Done(), func(got struct{}) string {
			return "mandatory transfer used the cancelled base context"
		})

		assert.Equal(t, tm.mandatoryCtx, retrieveCtx)

		close(releaseTransfer)
		requireWaitGroupDone(t, &tm.phase1Wg)
		assert.NoError(t, retrieveCtx.Err())
		requireNoTransferErrors(t, tm)
		requireFileContents(t, transfer.LocalPath, "log")
		client.AssertExpectations(t)
	})

	t.Run("reports receive file failure and does not record completed transfer", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, _, _ := newStartTransferTestManager(t, client, 1)
		transfer := fileTransfer(t, false)
		mockSingleListFiles(client, transfer.RemotePath)
		stream := &agentmocks.MockRetrieveFileStream{}
		stream.On("RecvMsg", mock.Anything).Return(errors.New("recv failed")).Once()
		client.On(
			"RetrieveFile",
			mock.Anything,
			&targetagentproto.FileRequest{Path: transfer.RemotePath},
			mock.Anything,
		).Return(stream, nil).Once()

		tm.startTransferRequest(TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client)})
		requireWaitGroupDone(t, &tm.baseWg)

		got := requireTransferError(t, tm, phase1TransferPhase, transfer)
		assert.Contains(t, got, "transfer failed")
		assert.Contains(t, got, "recv chunk: recv failed")
		assert.Empty(t, tm.completedTransfers)
		_, err := os.Stat(transfer.LocalPath)
		assert.True(t, os.IsNotExist(err), "expected failed transfer not to create final output file")
		client.AssertExpectations(t)
	})

	t.Run("transfer waits for slot before retrieving file, but startTransfer itself doesn't block", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, _, _ := newStartTransferTestManager(t, client, 1)
		transfer := fileTransfer(t, false)
		mockSingleListFiles(client, transfer.RemotePath)
		retrieveStarted := make(chan struct{}, 1)
		releaseTransfer := make(chan struct{})
		stream := &agentmocks.MockRetrieveFileStream{}
		stream.On("RecvMsg", mock.Anything).Run(func(args mock.Arguments) {
			<-releaseTransfer
		}).Return(io.EOF).Once()
		client.On("RetrieveFile", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			retrieveStarted <- struct{}{}
		}).Return(stream, nil).Once()

		// Fill up transfer slots.
		assert.NoError(t, tm.transferSlots.Acquire(context.Background(), phase1TransferPhase))

		startReturned := make(chan struct{})
		go func() {
			tm.startTransferRequest(TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client)})
			close(startReturned)
		}()

		requireMessage(t, startReturned, "startTransfer blocked while waiting for transfer slot capacity")

		requireNoMessage(t, retrieveStarted, func(got struct{}) string {
			return "RetrieveFile was called before a transfer slot was available"
		})

		assert.NoError(t, tm.transferSlots.Release())
		requireMessage(t, retrieveStarted, "RetrieveFile was not called after transfer slot capacity was released")

		close(releaseTransfer)
		requireWaitGroupDone(t, &tm.baseWg)
		requireNoTransferErrors(t, tm)
		client.AssertExpectations(t)
	})

	t.Run("increments wait group for globbed files while transfers are in progress and decrements when complete", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		tm, _, _, _ := newStartTransferTestManager(t, client, 3)
		requireWaitGroupDone(t, &tm.baseWg)
		transfer := globFileTransfer(t)
		remotePaths := []string{
			"/remote/first.txt",
			"/remote/second.txt",
		}
		mockListFiles(client, transfer.RemotePath, listFilesResponse(
			&targetagentproto.FileInfo{Path: remotePaths[0]},
			&targetagentproto.FileInfo{Path: remotePaths[1]},
		))
		var retrieveCallCount atomic.Int64
		retrieveStarted := make(chan string, len(remotePaths))
		releaseTransfers := make(chan struct{})
		for _, remote := range remotePaths {
			stream := &agentmocks.MockRetrieveFileStream{}
			stream.On("RecvMsg", mock.Anything).Run(func(args mock.Arguments) {
				<-releaseTransfers
			}).Return(errors.New("blocked transfer finished")).Once()
			client.On(
				"RetrieveFile",
				mock.Anything,
				mock.MatchedBy(func(req *targetagentproto.FileRequest) bool {
					return req.Path == remote
				}),
				mock.Anything,
			).Run(func(args mock.Arguments) {
				retrieveCallCount.Add(1)
				retrieveStarted <- args.Get(1).(*targetagentproto.FileRequest).Path
			}).Return(stream, nil).Once()
		}

		tm.startTransferRequest(TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client)})

		// Verify that both globbed transfers were started
		startedPaths := map[string]struct{}{}
		for range remotePaths {
			startedPath := requireMessage(t, retrieveStarted, "expected all globbed transfers to start")
			startedPaths[startedPath] = struct{}{}
		}
		assert.Equal(t, int64(2), retrieveCallCount.Load())
		assert.Equal(t, map[string]struct{}{
			remotePaths[0]: {},
			remotePaths[1]: {},
		}, startedPaths)
		// Verify that wait group is blocked while transfers are in progress
		requireWaitGroupBlocked(t, &tm.baseWg)

		// Verify that wait group releases on transfer completion
		close(releaseTransfers)
		requireWaitGroupDone(t, &tm.baseWg)
		assert.Len(t, tm.transferErrors.phase1Copy(), 2)
		client.AssertExpectations(t)
	})
}

func TestTransferManagerManifestUpdates(t *testing.T) {
	t.Run("failed manifest write does not update builder state", func(t *testing.T) {
		runCollection, err := run.NewRunCollection(t.TempDir())
		require.NoError(t, err)
		builder, err := runCollection.RunBuilder()
		require.NoError(t, err)
		writer := &runmocks.MockRunWriter{}
		writes := []run.RunBuilder{}
		// First write fails; second write succeeds and captures the persisted builder.
		writer.On("WriteManifest", mock.Anything).Return(errors.New("forced manifest write failure")).Once()
		writer.On("WriteManifest", mock.Anything).Run(func(args mock.Arguments) {
			writtenBuilder := args.Get(0).(run.RunBuilder)
			writes = append(writes, writtenBuilder.Clone())
		}).Return(nil).Once()
		updater := run.NewRunManifestUpdater(&builder, writer)
		componentType := cdf.ComponentType{Name: "data", SchemaVersion: "1.0"}

		err = updater.AddPendingComponent("entity/failed.txt", componentType)
		require.ErrorContains(t, err, "forced manifest write failure")
		require.Equal(t, 0, builder.ComponentCount())

		err = updater.AddPendingComponent("entity/success.txt", componentType)
		require.NoError(t, err)
		require.Equal(t, 1, builder.ComponentCount())
		require.False(t, builder.IsComponentPending("entity/failed.txt"))
		require.True(t, builder.IsComponentPending("entity/success.txt"))
		require.Len(t, writes, 1)
		require.False(t, writes[0].IsComponentPending("entity/failed.txt"))
		require.True(t, writes[0].IsComponentPending("entity/success.txt"))
		writer.AssertExpectations(t)
	})

	t.Run("deferred phase1 request writes pending through listen lifecycle then clears on flush", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		client := &targetagentmocks.TargetAgentClient{}
		runCollection, err := run.NewRunCollection(t.TempDir())
		require.NoError(t, err)
		builder, err := runCollection.RunBuilder()
		require.NoError(t, err)
		_, err = runCollection.CreateRun(builder, &cdf.Metadata{})
		require.NoError(t, err)
		runWriter := &run.ConcreteRunWriter{RunCollection: runCollection}
		tm := NewTransferManager(1, run.NewRunManifestUpdater(&builder, runWriter))
		logger := logrus.New()
		logger.SetOutput(io.Discard)

		go tm.Listen(logger, nil, &NullStageNotifier{})
		<-tm.ListeningStarted

		transfer := fileTransfer(t, false)
		transfer.ComponentType = cdf.ComponentType{Name: "data", SchemaVersion: "1.0"}
		request := TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client), ManifestRelativePath: "entity/file.txt"}

		tm.AddTransfer(request)
		require.Eventually(t, func() bool {
			model, err := runCollection.LoadRun(builder.RunID())
			if err != nil {
				return false
			}
			manifest := model.Manifest()
			entry := manifest.Lookup("entity/file.txt")
			return entry != nil && entry.Pending
		}, time.Second, time.Millisecond)

		mockSingleListFiles(client, transfer.RemotePath)
		mockRetrieveFile(client, transfer.RemotePath)

		got := tm.FlushTransfers(true)

		require.NoError(t, got)
		entry := requirePersistedManifestEntry(t, runCollection, builder.RunID(), "entity/file.txt")
		require.False(t, entry.Pending)
		client.AssertExpectations(t)
	})

	t.Run("phase1 success clears pending manifest entry", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		tm, runCollection, runID := newManifestTestTransferManager(t, client)
		transfer := fileTransfer(t, false)
		transfer.ComponentType = cdf.ComponentType{Name: "data", SchemaVersion: "1.0"}
		request := TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client), ManifestRelativePath: "entity/file.txt"}
		tm.addPendingManifestEntry(request)
		mockSingleListFiles(client, transfer.RemotePath)
		mockRetrieveFile(client, transfer.RemotePath)

		tm.startTransferRequest(request)
		tm.phase1Wg.Wait()

		entry := requirePersistedManifestEntry(t, runCollection, runID, "entity/file.txt")
		require.False(t, entry.Pending)
		require.Equal(t, transfer.ComponentType, entry.ComponentType)
		client.AssertExpectations(t)
	})

	t.Run("phase1 failure removes pending manifest entry", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		tm, runCollection, runID := newManifestTestTransferManager(t, client)
		transfer := fileTransfer(t, false)
		transfer.ComponentType = cdf.ComponentType{Name: "data", SchemaVersion: "1.0"}
		request := TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client), ManifestRelativePath: "entity/file.txt"}
		tm.addPendingManifestEntry(request)
		mockSingleListFiles(client, transfer.RemotePath)
		mockRetrieveFileError(client, transfer.RemotePath, context.Canceled)

		tm.startTransferRequest(request)
		tm.phase1Wg.Wait()

		requireNoPersistedManifestEntry(t, runCollection, runID, "entity/file.txt")
		client.AssertExpectations(t)
	})

	t.Run("missing agent supplier removes pending manifest entry", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		tm, runCollection, runID := newManifestTestTransferManager(t, client)
		transfer := fileTransfer(t, false)
		transfer.ComponentType = cdf.ComponentType{Name: "data", SchemaVersion: "1.0"}
		request := TransferRequest{FileTransfer: transfer, ManifestRelativePath: "entity/file.txt"}
		tm.addPendingManifestEntry(request)

		require.NotPanics(t, func() {
			tm.startTransferRequest(request)
		})

		requireNoPersistedManifestEntry(t, runCollection, runID, "entity/file.txt")
		errText := requireTransferError(t, tm, phase1TransferPhase, transfer)
		assert.Contains(t, errText, "transfer request has no agent supplier configured")
		client.AssertNotCalled(t, "ListFiles", mock.Anything, mock.Anything)
		client.AssertNotCalled(t, "RetrieveFile", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("background success writes pending then clears pending", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		tm, runCollection, runID := newManifestTestTransferManager(t, client)
		transfer := fileTransfer(t, false)
		transfer.ComponentType = cdf.ComponentType{Name: "data", SchemaVersion: "1.0"}
		request := TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client), ManifestRelativePath: "entity/background.txt", BackgroundTransfer: true}

		tm.addPendingManifestEntry(request)
		entry := requirePersistedManifestEntry(t, runCollection, runID, "entity/background.txt")
		require.True(t, entry.Pending)

		mockSingleListFiles(client, transfer.RemotePath)
		mockRetrieveFile(client, transfer.RemotePath)
		tm.startTransferRequest(request)
		tm.backgroundWg.Wait()

		entry = requirePersistedManifestEntry(t, runCollection, runID, "entity/background.txt")
		require.False(t, entry.Pending)
		client.AssertExpectations(t)
	})

	t.Run("background failure removes pending manifest entry", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		tm, runCollection, runID := newManifestTestTransferManager(t, client)
		transfer := fileTransfer(t, false)
		transfer.ComponentType = cdf.ComponentType{Name: "data", SchemaVersion: "1.0"}
		request := TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client), ManifestRelativePath: "entity/background.txt", BackgroundTransfer: true}

		tm.addPendingManifestEntry(request)
		mockSingleListFiles(client, transfer.RemotePath)
		mockRetrieveFileError(client, transfer.RemotePath, context.Canceled)

		tm.startTransferRequest(request)
		tm.backgroundWg.Wait()

		requireNoPersistedManifestEntry(t, runCollection, runID, "entity/background.txt")
		client.AssertExpectations(t)
	})

	t.Run("run failure removes skipped phase1 pending manifest entry", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		tm, runCollection, runID := newManifestTestTransferManager(t, client)
		transfer := fileTransfer(t, false)
		transfer.ComponentType = cdf.ComponentType{Name: "data", SchemaVersion: "1.0"}
		request := TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client), ManifestRelativePath: "entity/file.txt"}

		tm.addPendingManifestEntry(request)
		tm.flushTransfer(FlushTransfersMessage{runSucceeded: false}, request)

		requireNoPersistedManifestEntry(t, runCollection, runID, "entity/file.txt")
		client.AssertExpectations(t)
	})

	t.Run("deferred pending removal removes only entries still pending after phase1 completion", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		tm, runCollection, runID := newManifestTestTransferManager(t, client)

		phase1Transfer := fileTransfer(t, false)
		phase1Transfer.ComponentType = cdf.ComponentType{Name: "data", SchemaVersion: "1.0"}
		phase1Request := TransferRequest{FileTransfer: phase1Transfer, AgentSupplier: agentSupplier(client), ManifestRelativePath: "entity/file.txt"}
		completedBackgroundTransfer := fileTransfer(t, false)
		completedBackgroundTransfer.ComponentType = cdf.ComponentType{Name: "data", SchemaVersion: "1.0"}
		completedBackgroundRequest := TransferRequest{FileTransfer: completedBackgroundTransfer, AgentSupplier: agentSupplier(client), ManifestRelativePath: "entity/background-complete.txt", BackgroundTransfer: true}
		pendingBackgroundTransfer := fileTransfer(t, false)
		pendingBackgroundTransfer.ComponentType = cdf.ComponentType{Name: "data", SchemaVersion: "1.0"}
		pendingBackgroundRequest := TransferRequest{FileTransfer: pendingBackgroundTransfer, AgentSupplier: agentSupplier(client), ManifestRelativePath: "entity/background-pending.txt", BackgroundTransfer: true}
		tm.deferredPhase1Transfers = []TransferRequest{phase1Request}
		tm.deferredBackgroundTransfers = []TransferRequest{completedBackgroundRequest, pendingBackgroundRequest}

		tm.addPendingManifestEntry(phase1Request)
		tm.finishManifestRequest(phase1Request, true)
		tm.addPendingManifestEntry(completedBackgroundRequest)
		tm.finishManifestRequest(completedBackgroundRequest, true)
		tm.addPendingManifestEntry(pendingBackgroundRequest)

		tm.removeDeferredPendingManifestEntries()

		phase1Entry := requirePersistedManifestEntry(t, runCollection, runID, "entity/file.txt")
		require.False(t, phase1Entry.Pending)
		completedBackgroundEntry := requirePersistedManifestEntry(t, runCollection, runID, "entity/background-complete.txt")
		require.False(t, completedBackgroundEntry.Pending)
		requireNoPersistedManifestEntry(t, runCollection, runID, "entity/background-pending.txt")
	})

	t.Run("listen cancellation removes deferred background pending manifest entry", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		tm, runCollection, runID := newManifestTestTransferManager(t, client)
		tm.ListeningStarted = make(chan struct{})
		tm.listeningDone = make(chan struct{})
		tm.transferRequestChannel = make(chan AddTransferMessage)
		tm.flushTransfersChannel = make(chan FlushTransfersMessage)
		tm.transfersCompleteChannel = make(chan message.Message, 1)

		transfer := fileTransfer(t, false)
		transfer.ComponentType = cdf.ComponentType{Name: "data", SchemaVersion: "1.0"}
		request := TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client), ManifestRelativePath: "entity/background.txt", BackgroundTransfer: true}
		cancelChan := make(chan struct{})
		cmdStateChannel := &cmdsync.CommandStateChannel{CancelChan: cancelChan}
		listenReturned := make(chan struct{}, 1)

		go func() {
			tm.Listen(tm.observer.Logger, cmdStateChannel, &NullStageNotifier{})
			listenReturned <- struct{}{}
		}()
		<-tm.ListeningStarted

		tm.AddTransfer(request)
		entry := requirePersistedManifestEntry(t, runCollection, runID, "entity/background.txt")
		require.True(t, entry.Pending)

		close(cancelChan)
		requireMessage(t, listenReturned, "Listen did not return after cancellation")

		requireNoPersistedManifestEntry(t, runCollection, runID, "entity/background.txt")
		client.AssertExpectations(t)
	})

	t.Run("globbed background request updates original manifest entry after all transfers succeed", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		tm, runCollection, runID := newManifestTestTransferManager(t, client)
		transfer := globFileTransfer(t)
		transfer.ComponentType = cdf.ComponentType{Name: "data", SchemaVersion: "1.0"}
		request := TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client), ManifestRelativePath: "entity/*", BackgroundTransfer: true}

		tm.addPendingManifestEntry(request)
		mockListFiles(client, transfer.RemotePath, listFilesResponse(
			&targetagentproto.FileInfo{Path: "/remote/one.txt", Size: int64(len(testFileContents))},
			&targetagentproto.FileInfo{Path: "/remote/two.txt", Size: int64(len(testFileContents))},
			&targetagentproto.FileInfo{Path: "/remote/dir", IsDir: true},
		))
		mockRetrieveFile(client, "/remote/one.txt")
		mockRetrieveFile(client, "/remote/two.txt")

		tm.startTransferRequest(request)
		tm.backgroundWg.Wait()

		entry := requirePersistedManifestEntry(t, runCollection, runID, "entity/*")
		require.False(t, entry.Pending)
		client.AssertExpectations(t)
	})

	t.Run("globbed background no match removes pending manifest entry", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		tm, runCollection, runID := newManifestTestTransferManager(t, client)
		transfer := globFileTransfer(t)
		transfer.ComponentType = cdf.ComponentType{Name: "data", SchemaVersion: "1.0"}
		request := TransferRequest{FileTransfer: transfer, AgentSupplier: agentSupplier(client), ManifestRelativePath: "entity/*", BackgroundTransfer: true}

		tm.addPendingManifestEntry(request)
		mockListFiles(client, transfer.RemotePath, listFilesResponse(
			&targetagentproto.FileInfo{Path: transfer.RemotePath, Error: os.ErrNotExist.Error()},
		))

		tm.startTransferRequest(request)
		tm.backgroundWg.Wait()

		requireNoPersistedManifestEntry(t, runCollection, runID, "entity/*")
		client.AssertExpectations(t)
	})
}

func TestTransferManagerRollbackTransfer(t *testing.T) {
	t.Run("removes local file and logs rollback start", func(t *testing.T) {
		tm, logBuf := newTransferManagerWithLogBuffer()
		transfer := fileTransfer(t, false)
		assert.NoError(t, os.WriteFile(transfer.LocalPath, []byte("retrieved data"), 0o600))

		tm.rollbackTransfer(transfer)

		_, err := os.Stat(transfer.LocalPath)
		assert.True(t, os.IsNotExist(err), "expected rollback to remove local file, got %v", err)
		logOutput := logBuf.String()
		requireLogContains(t, logOutput, "[TransferManager] Reverting transfer: '/remote/output.txt' -> '"+transfer.LocalPath+"'")
		assert.NotContains(t, logOutput, "[TransferManager] Failed to revert transfer")
	})

	t.Run("logs rollback error when local file cannot be removed", func(t *testing.T) {
		tm, logBuf := newTransferManagerWithLogBuffer()
		transfer := fileTransfer(t, false)

		tm.rollbackTransfer(transfer)

		logOutput := logBuf.String()
		requireLogContains(t, logOutput, "[TransferManager] Reverting transfer: '"+transfer.RemotePath+"' -> '"+transfer.LocalPath+"'")
		requireLogContains(t, logOutput, "[TransferManager] Failed to revert transfer: '"+transfer.RemotePath+"' -> '"+transfer.LocalPath+"'")
		requireLogContains(t, logOutput, "remove "+transfer.LocalPath)
	})
}

func TestTransferManagerTransferErrors(t *testing.T) {
	t.Run("records error and logs transfer failure", func(t *testing.T) {
		tm, logBuf := newTransferManagerWithLogBuffer()
		tm.transferErrors = newTransferErrors()
		transfer := fileTransfer(t, false)
		transferErr := errors.New("download failed")

		tm.recordTransferError(transferErr, transfer, phase1TransferPhase)

		assert.Equal(t, map[transferErrorKey]error{
			newTransferErrorKey(transfer): transferErr,
		}, tm.transferErrors.phase1Copy())
		assert.Empty(t, tm.transferErrors.backgroundCopy())
		requireLogContains(t, logBuf.String(), "[TransferManager] Transfer failed: '"+transfer.RemotePath+"' -> '"+transfer.LocalPath+"': download failed")
	})

	t.Run("records phase1 and background errors separately", func(t *testing.T) {
		tm, logBuf := newTransferManagerWithLogBuffer()
		tm.transferErrors = newTransferErrors()
		firstTransfer := fileTransfer(t, false)
		firstTransfer.RemotePath = "/remote/first.txt"
		secondTransfer := fileTransfer(t, false)
		secondTransfer.RemotePath = "/remote/second.txt"
		firstErr := errors.New("first failed")
		secondErr := errors.New("second failed")

		tm.recordTransferError(firstErr, firstTransfer, phase1TransferPhase)
		tm.recordTransferError(secondErr, secondTransfer, backgroundTransferPhase)

		assert.Equal(t, map[transferErrorKey]error{
			newTransferErrorKey(firstTransfer): firstErr,
		}, tm.transferErrors.phase1Copy())
		assert.Equal(t, map[transferErrorKey]error{
			newTransferErrorKey(secondTransfer): secondErr,
		}, tm.transferErrors.backgroundCopy())
		assert.Equal(t, map[transferErrorKey]error{
			newTransferErrorKey(firstTransfer):  firstErr,
			newTransferErrorKey(secondTransfer): secondErr,
		}, tm.transferErrors.combined())
		logOutput := logBuf.String()
		requireLogContains(t, logOutput, "[TransferManager] Transfer failed: '"+firstTransfer.RemotePath+"' -> '"+firstTransfer.LocalPath+"': first failed")
		requireLogContains(t, logOutput, "[TransferManager] Transfer failed: '"+secondTransfer.RemotePath+"' -> '"+secondTransfer.LocalPath+"': second failed")
	})

	t.Run("returns defensive copies", func(t *testing.T) {
		errs := newTransferErrors()
		firstTransfer := conductor.FileTransfer{RemotePath: "/remote/first.txt", LocalPath: "/local/first.txt"}
		secondTransfer := conductor.FileTransfer{RemotePath: "/remote/second.txt", LocalPath: "/local/second.txt"}
		firstErr := errors.New("first failed")
		secondErr := errors.New("second failed")
		errs.add(phase1TransferPhase, firstTransfer, firstErr)
		errs.add(backgroundTransferPhase, secondTransfer, secondErr)

		phase1Copy := errs.phase1Copy()
		backgroundCopy := errs.backgroundCopy()
		combinedCopy := errs.combined()

		phase1Copy[newTransferErrorKey(firstTransfer)] = errors.New("mutated")
		backgroundCopy[newTransferErrorKey(secondTransfer)] = errors.New("mutated")
		combinedCopy[newTransferErrorKey(conductor.FileTransfer{RemotePath: "/remote/third.txt", LocalPath: "/local/third.txt"})] = errors.New("mutated")

		assert.Equal(t, map[transferErrorKey]error{
			newTransferErrorKey(firstTransfer): firstErr,
		}, errs.phase1Copy())
		assert.Equal(t, map[transferErrorKey]error{
			newTransferErrorKey(secondTransfer): secondErr,
		}, errs.backgroundCopy())
		assert.NotContains(t, errs.combined(), newTransferErrorKey(conductor.FileTransfer{RemotePath: "/remote/third.txt", LocalPath: "/local/third.txt"}))
	})
}

func TestTransferManagerCopyFromTargetAndWait(t *testing.T) {
	client := &targetagentmocks.TargetAgentClient{}
	tm, _, _, _ := newStartTransferTestManager(t, client, 1)
	tm.ListeningStarted = make(chan struct{})
	close(tm.ListeningStarted)
	tm.transferRequestChannel = make(chan AddTransferMessage, 1)
	tm.listeningDone = make(chan struct{})
	transfer := fileTransfer(t, false)
	mockSingleListFiles(client, transfer.RemotePath)
	mockRetrieveFile(client, transfer.RemotePath)

	done := make(chan error, 1)
	go func() {
		done <- tm.CopyFromTargetAndWait(transfer, agentSupplier(client))
	}()

	msg := requireMessage(t, tm.transferRequestChannel, "CopyFromTargetAndWait did not send transfer request")
	assert.Equal(t, transfer, msg.t.FileTransfer)
	assert.NotNil(t, msg.t.AgentSupplier)
	assert.True(t, msg.t.ImmediateRetrieval)

	close(msg.confirm)
	requireNoMessage(t, done, func(got error) string {
		return "CopyFromTargetAndWait returned before the transfer completed"
	})

	tm.startTransferRequest(msg.t)
	requireWaitGroupDone(t, &tm.baseWg)

	require.NoError(t, requireMessage(t, done, "CopyFromTargetAndWait did not return"))
	requireFileContents(t, transfer.LocalPath, testFileContents)
	client.AssertExpectations(t)
}
