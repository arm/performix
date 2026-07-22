// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	agentmocks "github.com/Arm-Debug/apap-cli/apap-engine/agent/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	targetagentmocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

func startTestTransferManager(t *testing.T, client *targetagentmocks.TargetAgentClient, cancelChan chan struct{}) *recipe.TransferManager {
	t.Helper()

	tm := recipe.NewTransferManager(1, nil)

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	go tm.Listen(logger, &cmdsync.CommandStateChannel{CancelChan: cancelChan}, &recipe.NullStageNotifier{})
	<-tm.ListeningStarted

	return tm
}

func testTransfer(t *testing.T, remotePath string) conductor.FileTransfer {
	t.Helper()
	return conductor.FileTransfer{
		RemotePath: remotePath,
		LocalPath:  filepath.Join(t.TempDir(), "out.txt"),
	}
}

func mockListFilesError(client *targetagentmocks.TargetAgentClient, remotePath string) {
	client.On(
		"ListFiles",
		mock.Anything,
		&targetagentproto.ListFilesRequest{Paths: []string{remotePath}},
	).Return(nil, errors.New("list failed")).Once()
}

func requireMessage[T any](t *testing.T, ch <-chan T, msg string) T {
	t.Helper()

	var empty T
	select {
	case got := <-ch:
		return got
	case <-time.After(time.Second):
		t.Fatal(msg)
	}
	return empty
}

func mockRetrieveFile(client *targetagentmocks.TargetAgentClient, remotePath string) {
	stream := &agentmocks.MockRetrieveFileStream{}
	agentmocks.SetStreamRecv(stream, "contents", nil)
	agentmocks.SetStreamRecv(stream, "", io.EOF)

	client.On(
		"RetrieveFile",
		mock.Anything,
		&targetagentproto.FileRequest{Path: remotePath},
		mock.Anything,
	).Return(stream, nil).Once()
}

func mockBlockingRetrieveUntilCancelled(client *targetagentmocks.TargetAgentClient, remotePath string) chan struct{} {
	retrieveFileCalled := make(chan struct{}, 1)
	client.On(
		"ListFiles",
		mock.Anything,
		&targetagentproto.ListFilesRequest{Paths: []string{remotePath}},
	).Return(&targetagentproto.ListFilesResponse{Responses: []*targetagentproto.FileInfos{
		{FileInfos: []*targetagentproto.FileInfo{{Path: remotePath, Size: 1}}},
	}}, nil).Once()

	streamCtx := make(chan context.Context, 1)
	stream := &agentmocks.MockRetrieveFileStream{}
	stream.On("RecvMsg", mock.Anything).Run(func(mock.Arguments) {
		ctx := <-streamCtx
		retrieveFileCalled <- struct{}{}
		<-ctx.Done()
	}).Return(errors.New("transfer cancelled")).Once()

	client.On(
		"RetrieveFile",
		mock.Anything,
		&targetagentproto.FileRequest{Path: remotePath},
		mock.Anything,
	).Run(func(args mock.Arguments) {
		streamCtx <- args.Get(0).(context.Context)
	}).Return(stream, nil).Once()

	return retrieveFileCalled
}

func TestWaitForTransfersStageErrorType(t *testing.T) {
	t.Run("phase1 completion uses UpdateRunResult before background finishes", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		cancelChan := make(chan struct{})
		tm := startTestTransferManager(t, client, cancelChan)
		stage := NewWaitForTransfersStage(tm)

		runCollection, err := run.NewRunCollection(t.TempDir())
		require.NoError(t, err)
		builder, err := runCollection.RunBuilder()
		require.NoError(t, err)
		builder.AddEntity("fakeEntity")
		runID, err := runCollection.CreateRun(builder, &cdf.Metadata{
			RunResult:    string(run.RecipeInProgress),
			EndTime:      util.InvalidTime(),
			WorkloadType: "Launch",
		})
		require.NoError(t, err)

		backgroundTransfer := testTransfer(t, "/remote/background.txt")
		retrieveFileCalled := mockBlockingRetrieveUntilCancelled(client, backgroundTransfer.RemotePath)
		tm.AddTransfer(recipe.TransferRequest{
			FileTransfer:       backgroundTransfer,
			ImmediateRetrieval: false,
			BackgroundTransfer: true,
			AgentSupplier:      func() *agent.AgentConn { return &agent.AgentConn{Client: client} },
		})

		errCh := make(chan error, 1)
		go func() {
			_, err := stage.Execute(&recipe.StageContext{
				Context:       context.Background(),
				RunID:         runID,
				RunCollection: runCollection,
			})
			errCh <- err
		}()

		requireMessage(t, retrieveFileCalled, "background transfer did not start")
		desc, err := runCollection.RunDescription(context.Background(), runID)
		require.NoError(t, err)
		require.Equal(t, string(run.RecipeInProgressPhase1Complete), desc.RunResult)
		require.NotEqual(t, util.InvalidTime(), desc.EndTime)

		close(cancelChan)
		require.ErrorIs(t, <-errCh, message.New(message.EngineCommonUserCancellationError))
		client.AssertExpectations(t)
	})

	t.Run("background error after required transfer success uses phase1-complete failure result", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		tm := startTestTransferManager(t, client, make(chan struct{}))
		stage := NewWaitForTransfersStage(tm)

		phase1Transfer := testTransfer(t, "/remote/phase1.txt")
		client.On(
			"ListFiles",
			mock.Anything,
			&targetagentproto.ListFilesRequest{Paths: []string{phase1Transfer.RemotePath}},
		).Return(&targetagentproto.ListFilesResponse{Responses: []*targetagentproto.FileInfos{
			{FileInfos: []*targetagentproto.FileInfo{{Path: phase1Transfer.RemotePath, Size: 1}}},
		}}, nil).Once()
		mockRetrieveFile(client, phase1Transfer.RemotePath)
		tm.AddTransfer(recipe.TransferRequest{
			FileTransfer:       phase1Transfer,
			ImmediateRetrieval: false,
			AgentSupplier:      func() *agent.AgentConn { return &agent.AgentConn{Client: client} },
		})

		backgroundTransfer := testTransfer(t, "/remote/background.txt")
		mockListFilesError(client, backgroundTransfer.RemotePath)
		tm.AddTransfer(recipe.TransferRequest{
			FileTransfer:       backgroundTransfer,
			ImmediateRetrieval: false,
			BackgroundTransfer: true,
			AgentSupplier:      func() *agent.AgentConn { return &agent.AgentConn{Client: client} },
		})

		_, err := stage.Execute(&recipe.StageContext{})

		require.Error(t, err)
		require.Equal(t, run.RecipeFailureRetrievePhase1Complete, stage.ErrorType())
		client.AssertExpectations(t)
	})

	t.Run("phase1 error keeps regular retrieve failure result", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		tm := startTestTransferManager(t, client, make(chan struct{}))
		stage := NewWaitForTransfersStage(tm)

		phase1Transfer := testTransfer(t, "/remote/phase1.txt")
		mockListFilesError(client, phase1Transfer.RemotePath)
		tm.AddTransfer(recipe.TransferRequest{
			FileTransfer:       phase1Transfer,
			ImmediateRetrieval: false,
			AgentSupplier:      func() *agent.AgentConn { return &agent.AgentConn{Client: client} },
		})

		_, err := stage.Execute(&recipe.StageContext{})

		require.Error(t, err)
		require.Equal(t, run.RecipeFailureRetrieve, stage.ErrorType())
		client.AssertExpectations(t)
	})

	t.Run("cancellation after phase1 completion uses phase1-complete failure result", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		cancelChan := make(chan struct{})
		tm := startTestTransferManager(t, client, cancelChan)
		stage := NewWaitForTransfersStage(tm)

		backgroundTransfer := testTransfer(t, "/remote/background.txt")
		retrieveFileCalled := mockBlockingRetrieveUntilCancelled(client, backgroundTransfer.RemotePath)
		tm.AddTransfer(recipe.TransferRequest{
			FileTransfer:       backgroundTransfer,
			ImmediateRetrieval: false,
			BackgroundTransfer: true,
			AgentSupplier:      func() *agent.AgentConn { return &agent.AgentConn{Client: client} },
		})

		errCh := make(chan error, 1)
		go func() {
			_, err := stage.Execute(&recipe.StageContext{})
			errCh <- err
		}()

		requireMessage(t, retrieveFileCalled, "background transfer did not start")
		close(cancelChan)

		err := <-errCh
		require.ErrorIs(t, err, message.New(message.EngineCommonUserCancellationError))
		require.Equal(t, run.RecipeFailureRetrievePhase1Complete, stage.ErrorType())
		client.AssertExpectations(t)
	})
}
