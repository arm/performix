// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/agent/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	targetagentmocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

type stageNotifier struct {
	*recipe.NullStageNotifier
	received atomic.Int64
}

func (n *stageNotifier) OnStageProgress(_ notifiers.StageInfo, stageProgress notifiers.StageProgress) {
	n.received.Store(stageProgress.Sent)
}

type testStage struct {
	stage        *RetrieveAgentFilesStage
	stageCtx     *recipe.StageContext
	notifier     *stageNotifier
	logPath      string
	dataPath     string
	logContents  string
	dataContents string
	logStream    *mocks.MockRetrieveFileStream
	dataStream   *mocks.MockRetrieveFileStream
	tAgent       *targetagentmocks.TargetAgentClient
}

func newTestStage(t *testing.T, expectDataTransfer bool) testStage {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "out", "log.txt")
	dataPath := filepath.Join(tmp, "out", "collection.txt")
	logRemote := "/remote/log.txt"
	dataRemote := "/remote/collection.txt"
	logContents := "log file contents"
	dataContents := "data file contents"

	logStream := &mocks.MockRetrieveFileStream{}
	mocks.SetStreamRecv(logStream, logContents, nil)
	mocks.SetStreamRecv(logStream, "", io.EOF)

	dataStream := &mocks.MockRetrieveFileStream{}
	mocks.SetStreamRecv(dataStream, dataContents, nil)
	mocks.SetStreamRecv(dataStream, "", io.EOF)

	tAgent := &targetagentmocks.TargetAgentClient{}
	tAgent.
		On("RetrieveFile", mock.Anything, mock.MatchedBy(func(req *targetagentproto.FileRequest) bool {
			return req.Path == logRemote
		}), mock.Anything).
		Return(logStream, nil).
		Once()
	if expectDataTransfer {
		tAgent.
			On("RetrieveFile", mock.Anything, mock.MatchedBy(func(req *targetagentproto.FileRequest) bool {
				return req.Path == dataRemote
			}), mock.Anything).
			Return(dataStream, nil).
			Once()
	}
	listResponses := []*targetagentproto.FileInfos{
		{FileInfos: []*targetagentproto.FileInfo{{Path: logRemote, Size: int64(len(logContents))}}},
	}
	if expectDataTransfer {
		listResponses = append(listResponses, &targetagentproto.FileInfos{
			FileInfos: []*targetagentproto.FileInfo{{Path: dataRemote, Size: int64(len(dataContents))}},
		})
	}
	tAgent.On("ListFiles", mock.Anything, mock.Anything).
		Return(&targetagentproto.ListFilesResponse{Responses: listResponses}, nil).
		Once()

	sn := &stageNotifier{}

	stage := &RetrieveAgentFilesStage{
		RecipeCollector: &recipe.Collector{
			FileRetriever: &recipe.RetrieveAgentFilesStageRetriever{
				FileTransfers: []recipe.TransferRequest{
					{FileTransfer: conductor.FileTransfer{LocalPath: dataPath, RemotePath: dataRemote}, AgentSupplier: func() *agent.AgentConn { return &agent.AgentConn{Client: tAgent} }},
				},
				LogTransfers: []recipe.TransferRequest{
					{FileTransfer: conductor.FileTransfer{LocalPath: logPath, RemotePath: logRemote}, AgentSupplier: func() *agent.AgentConn { return &agent.AgentConn{Client: tAgent} }},
				},
			},
		},
	}

	stageCtx := &recipe.StageContext{Context: context.Background(), CommandStateChannel: &cmdsync.CommandStateChannel{
		CancelChan: make(chan struct{}),
		StopChan:   make(chan struct{}),
	}, StageNotifier: sn}

	return testStage{
		stage:        stage,
		stageCtx:     stageCtx,
		notifier:     sn,
		logPath:      logPath,
		dataPath:     dataPath,
		logContents:  logContents,
		dataContents: dataContents,
		logStream:    logStream,
		dataStream:   dataStream,
		tAgent:       tAgent,
	}
}

func TestRetrieveAgentFilesStage_Execute(t *testing.T) {
	t.Run("no transfers returns (nil, nil)", func(t *testing.T) {
		stage := &RetrieveAgentFilesStage{
			RecipeCollector: &recipe.Collector{FileRetriever: &recipe.RetrieveAgentFilesStageRetriever{}},
		}
		stageCtx := &recipe.StageContext{Context: context.Background()}

		cleanup, err := stage.Execute(stageCtx)
		require.NoError(t, err)
		require.Nil(t, cleanup)
	})

	t.Run("CancelChan cancellation before Acquire propagates", func(t *testing.T) {
		tmp := t.TempDir()
		local := filepath.Join(tmp, "x.txt")
		remote := "/any"

		cancel := make(chan struct{})
		close(cancel) // already closed => cancel() triggers immediately inside Execute

		stage := &RetrieveAgentFilesStage{
			RecipeCollector: &recipe.Collector{
				FileRetriever: &recipe.RetrieveAgentFilesStageRetriever{
					FileTransfers: []recipe.TransferRequest{
						{FileTransfer: conductor.FileTransfer{LocalPath: local, RemotePath: remote}, AgentSupplier: func() *agent.AgentConn { return &agent.AgentConn{Client: &targetagentmocks.TargetAgentClient{}} }},
					},
				},
			},
		}
		stageCtx := &recipe.StageContext{Context: context.Background(), CommandStateChannel: &cmdsync.CommandStateChannel{
			CancelChan: cancel,
		}}

		cleanup, err := stage.Execute(stageCtx)
		require.Nil(t, cleanup)
		expectedErr := message.New(message.EngineCommonUserCancellationError)
		require.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("happy path writes multiple files, progress is reported", func(t *testing.T) {
		s := newTestStage(t, true)

		_, err := s.stage.Execute(s.stageCtx)
		require.NoError(t, err)

		data, readErr := os.ReadFile(s.logPath)
		require.NoError(t, readErr)
		assert.Equal(t, s.logContents, string(data))

		data, readErr = os.ReadFile(s.dataPath)
		require.NoError(t, readErr)
		assert.Equal(t, s.dataContents, string(data))

		assert.Equal(t, s.notifier.received.Load(), int64(len(s.logContents)+len(s.dataContents)))

		s.tAgent.AssertExpectations(t)
		s.logStream.AssertExpectations(t)
		s.dataStream.AssertExpectations(t)
	})

	t.Run("uses per-transfer source agent supplier", func(t *testing.T) {
		tmp := t.TempDir()
		localPath := filepath.Join(tmp, "out", "host.txt")
		remotePath := "/host/output.txt"
		fileContents := "host locality contents"

		hostStream := &mocks.MockRetrieveFileStream{}
		mocks.SetStreamRecv(hostStream, fileContents, nil)
		mocks.SetStreamRecv(hostStream, "", io.EOF)

		hostAgent := &targetagentmocks.TargetAgentClient{}
		hostAgent.On("ListFiles", mock.Anything, &targetagentproto.ListFilesRequest{Paths: []string{remotePath}}).
			Return(&targetagentproto.ListFilesResponse{
				Responses: []*targetagentproto.FileInfos{
					{FileInfos: []*targetagentproto.FileInfo{{Path: remotePath, Size: int64(len(fileContents))}}},
				},
			}, nil).
			Once()
		hostAgent.On("RetrieveFile", mock.Anything, &targetagentproto.FileRequest{Path: remotePath}, mock.Anything).
			Return(hostStream, nil).
			Once()

		stage := &RetrieveAgentFilesStage{
			RecipeCollector: &recipe.Collector{
				FileRetriever: &recipe.RetrieveAgentFilesStageRetriever{
					FileTransfers: []recipe.TransferRequest{
						{FileTransfer: conductor.FileTransfer{LocalPath: localPath, RemotePath: remotePath}, AgentSupplier: func() *agent.AgentConn { return &agent.AgentConn{Client: hostAgent} }},
					},
				},
			},
		}

		_, err := stage.Execute(&recipe.StageContext{
			Context: context.Background(),
			CommandStateChannel: &cmdsync.CommandStateChannel{
				CancelChan: make(chan struct{}),
				StopChan:   make(chan struct{}),
			},
		})
		require.NoError(t, err)

		data, readErr := os.ReadFile(localPath)
		require.NoError(t, readErr)
		assert.Equal(t, fileContents, string(data))

		hostAgent.AssertExpectations(t)
		hostStream.AssertExpectations(t)
	})

	t.Run("transfers in exclude list are skipped", func(t *testing.T) {
		tmp := t.TempDir()
		includedRemote := "/remote/included.txt"
		excludedRemote := "/remote/excluded.txt"
		excludeGlob := "/remote/exc*.txt"

		includedStream := &mocks.MockRetrieveFileStream{}
		mocks.SetStreamRecv(includedStream, "contents", nil)
		mocks.SetStreamRecv(includedStream, "", io.EOF)

		tAgent := &targetagentmocks.TargetAgentClient{}
		tAgent.On("ListFiles", mock.Anything, mock.Anything).
			Return(&targetagentproto.ListFilesResponse{
				Responses: []*targetagentproto.FileInfos{
					{FileInfos: []*targetagentproto.FileInfo{
						{Path: includedRemote, Size: int64(len("contents"))},
						{Path: excludedRemote, Size: 1},
					}},
				},
			}, nil).
			Once()
		tAgent.
			On("RetrieveFile", mock.Anything, mock.MatchedBy(func(req *targetagentproto.FileRequest) bool {
				return req.Path == includedRemote
			}), mock.Anything).
			Return(includedStream, nil).
			Once()

		stage := &RetrieveAgentFilesStage{
			RecipeCollector: &recipe.Collector{
				FileRetriever: &recipe.RetrieveAgentFilesStageRetriever{
					FileTransfers: []recipe.TransferRequest{
						{FileTransfer: conductor.FileTransfer{LocalPath: filepath.Join(tmp, "out", "*"), RemotePath: "/remote/*", Exclude: []string{excludeGlob}}, AgentSupplier: func() *agent.AgentConn { return &agent.AgentConn{Client: tAgent} }},
					},
				},
			},
		}
		stageCtx := &recipe.StageContext{Context: context.Background(), CommandStateChannel: &cmdsync.CommandStateChannel{
			StopChan: make(chan struct{}),
		}}

		_, err := stage.Execute(stageCtx)
		require.NoError(t, err)

		data, readErr := os.ReadFile(filepath.Join(tmp, "out", "included.txt"))
		require.NoError(t, readErr)
		assert.Equal(t, "contents", string(data))

		_, readErr = os.ReadFile(filepath.Join(tmp, "out", "excluded.txt"))
		require.Error(t, readErr)

		tAgent.AssertExpectations(t)
		includedStream.AssertExpectations(t)
	})

	t.Run("upon stage error, only log files are relocated", func(t *testing.T) {
		s := newTestStage(t, false)

		s.stageCtx.Failure = recipe.StageContextFailure{Error: errors.New("run error")}

		_, err := s.stage.Execute(s.stageCtx)
		require.NoError(t, err)

		data, readErr := os.ReadFile(s.logPath)
		require.NoError(t, readErr)
		assert.Equal(t, s.logContents, string(data))

		// Data file should not exist
		_, readErr = os.ReadFile(s.dataPath)
		require.Error(t, readErr)

		assert.Equal(t, s.notifier.received.Load(), int64(len(s.logContents)))

		s.tAgent.AssertExpectations(t)
		s.logStream.AssertExpectations(t)
	})

	t.Run("list file error is propagated", func(t *testing.T) {

		tAgent := &targetagentmocks.TargetAgentClient{}
		tAgent.On("ListFiles", mock.Anything, mock.Anything).Return(nil, errors.New("Boom!")).Once()

		stage := &RetrieveAgentFilesStage{
			RecipeCollector: &recipe.Collector{
				FileRetriever: &recipe.RetrieveAgentFilesStageRetriever{
					FileTransfers: []recipe.TransferRequest{
						{FileTransfer: conductor.FileTransfer{LocalPath: "", RemotePath: "remote"}, AgentSupplier: func() *agent.AgentConn { return &agent.AgentConn{Client: tAgent} }},
					},
				},
			},
		}
		stageCtx := &recipe.StageContext{Context: context.Background(), CommandStateChannel: &cmdsync.CommandStateChannel{
			StopChan: make(chan struct{}),
		}}

		_, err := stage.Execute(stageCtx)
		expectedErr := message.New(message.CommonUnknownError).WithCause(errors.New("Boom!"))
		require.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))

		tAgent.AssertExpectations(t)
	})

	t.Run("individual list file error is propagated", func(t *testing.T) {

		tAgent := &targetagentmocks.TargetAgentClient{}
		fi := &targetagentproto.ListFilesResponse{Responses: []*targetagentproto.FileInfos{{}}}
		fi.Responses[0] = &targetagentproto.FileInfos{FileInfos: []*targetagentproto.FileInfo{
			{Path: "remote", Error: "Boom!"},
		}}
		tAgent.On("ListFiles", mock.Anything, mock.Anything).Return(fi, nil).Once()

		stage := &RetrieveAgentFilesStage{
			RecipeCollector: &recipe.Collector{
				FileRetriever: &recipe.RetrieveAgentFilesStageRetriever{
					FileTransfers: []recipe.TransferRequest{
						{FileTransfer: conductor.FileTransfer{LocalPath: "", RemotePath: ""}, AgentSupplier: func() *agent.AgentConn { return &agent.AgentConn{Client: tAgent} }},
					},
				},
			},
		}
		stageCtx := &recipe.StageContext{Context: context.Background(), CommandStateChannel: &cmdsync.CommandStateChannel{
			StopChan: make(chan struct{}),
		}}

		_, err := stage.Execute(stageCtx)
		expectedMetadata := map[string]string{
			"filePath": "remote",
			"error":    "file does not exist on target: \"Boom!\"",
		}
		expectedErr := message.New(message.EngineRecipeStagesRetrieveAgentFilesRetrieveFile).WithMetadata(expectedMetadata)
		require.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))

		tAgent.AssertExpectations(t)
	})

	t.Run("individual list file errors are propagated", func(t *testing.T) {

		tAgent := &targetagentmocks.TargetAgentClient{}
		fi := &targetagentproto.ListFilesResponse{Responses: make([]*targetagentproto.FileInfos, 2)}
		fi.Responses[0] = &targetagentproto.FileInfos{FileInfos: []*targetagentproto.FileInfo{
			{Path: "remote1", Error: "Boom!"},
		}}
		fi.Responses[1] = &targetagentproto.FileInfos{FileInfos: []*targetagentproto.FileInfo{
			{Path: "remote2", Error: "Boom 2!"},
		}}
		tAgent.On("ListFiles", mock.Anything, mock.Anything).Return(fi, nil).Once()

		stage := &RetrieveAgentFilesStage{
			RecipeCollector: &recipe.Collector{
				FileRetriever: &recipe.RetrieveAgentFilesStageRetriever{
					FileTransfers: []recipe.TransferRequest{
						{FileTransfer: conductor.FileTransfer{LocalPath: "", RemotePath: ""}, AgentSupplier: func() *agent.AgentConn { return &agent.AgentConn{Client: tAgent} }},
						{FileTransfer: conductor.FileTransfer{LocalPath: "", RemotePath: ""}, AgentSupplier: func() *agent.AgentConn { return &agent.AgentConn{Client: tAgent} }},
					},
				},
			},
		}
		stageCtx := &recipe.StageContext{Context: context.Background(), CommandStateChannel: &cmdsync.CommandStateChannel{
			StopChan: make(chan struct{}),
		}}

		_, err := stage.Execute(stageCtx)
		expectedMetadata := map[string]string{
			"numFiles": "2",
			"remote1":  "file does not exist on target: \"Boom!\"",
			"remote2":  "file does not exist on target: \"Boom 2!\"",
		}
		expectedErr := message.New(message.EngineRecipeStagesRetrieveAgentFilesRetrieveFiles).WithMetadata(expectedMetadata)
		require.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))

		tAgent.AssertExpectations(t)
	})

	t.Run("file retrieval errors collected on large glob expansions", func(t *testing.T) {
		const errsCount = 8
		fileInfos := make([]*targetagentproto.FileInfo, 0, errsCount)
		for i := 0; i < errsCount; i++ {
			fileInfos = append(fileInfos, &targetagentproto.FileInfo{
				Path:  fmt.Sprintf("some-file-%d", i),
				Error: fmt.Sprintf("some reason %d!", i),
			})
		}

		tAgent := &targetagentmocks.TargetAgentClient{}
		tAgent.On("ListFiles", mock.Anything, mock.Anything).
			Return(&targetagentproto.ListFilesResponse{
				Responses: []*targetagentproto.FileInfos{
					{FileInfos: fileInfos},
				},
			}, nil).Once()

		// 1 file transfer will result in an errCh of capacity 1
		// but we generate 8 errors to exceed that capacity (simulates globs expanding to many files)
		// to ensure that error consuming works even when errCh is exceeded
		stage := &RetrieveAgentFilesStage{
			RecipeCollector: &recipe.Collector{
				FileRetriever: &recipe.RetrieveAgentFilesStageRetriever{
					FileTransfers: []recipe.TransferRequest{
						{FileTransfer: conductor.FileTransfer{LocalPath: "", RemotePath: "some-path-*"}, AgentSupplier: func() *agent.AgentConn { return &agent.AgentConn{Client: tAgent} }},
					},
				},
			},
		}
		stageCtx := &recipe.StageContext{Context: context.Background(),
			CommandStateChannel: &cmdsync.CommandStateChannel{
				StopChan: make(chan struct{}),
			},
		}

		_, err := stage.Execute(stageCtx)
		require.Error(t, err)

		var msg *message.MessageImpl
		require.ErrorAs(t, err, &msg)
		assert.Equal(t, message.EngineRecipeStagesRetrieveAgentFilesRetrieveFiles, msg.Code())

		metadata := msg.Metadata()
		assert.Equal(t, errsCount, len(metadata)-1)
		assert.Equal(t, fmt.Sprintf("%d", errsCount), metadata["numFiles"])

		for i := 0; i < errsCount; i++ {
			key := fmt.Sprintf("some-file-%d", i)
			val, exists := metadata[key]
			require.True(t, exists)
			assert.Contains(t, val, fmt.Sprintf("some reason %d!", i))
		}

		tAgent.AssertExpectations(t)
	})

	t.Run("transfer errors are ignored if a previous stage failed", func(t *testing.T) {

		tAgent := &targetagentmocks.TargetAgentClient{}
		fi := &targetagentproto.ListFilesResponse{Responses: []*targetagentproto.FileInfos{{}}}
		fi.Responses[0] = &targetagentproto.FileInfos{FileInfos: []*targetagentproto.FileInfo{
			{Path: "remote", Error: "Boom!"},
		}}
		tAgent.On("ListFiles", mock.Anything, mock.Anything).Return(fi, nil).Once()
		stage := &RetrieveAgentFilesStage{
			RecipeCollector: &recipe.Collector{
				FileRetriever: &recipe.RetrieveAgentFilesStageRetriever{
					LogTransfers: []recipe.TransferRequest{
						{FileTransfer: conductor.FileTransfer{LocalPath: "", RemotePath: ""}, AgentSupplier: func() *agent.AgentConn { return &agent.AgentConn{Client: tAgent} }},
					},
				},
			},
		}
		stageCtx := &recipe.StageContext{
			Context: context.Background(),
			Failure: recipe.StageContextFailure{Error: errors.New("run error")},
			CommandStateChannel: &cmdsync.CommandStateChannel{
				StopChan: make(chan struct{}),
			},
		}

		_, err := stage.Execute(stageCtx)
		require.NoError(t, err)

		tAgent.AssertExpectations(t)
	})

	t.Run("structured wildcard with no matches is skipped", func(t *testing.T) {
		tAgent := &targetagentmocks.TargetAgentClient{}
		tAgent.On("ListFiles", mock.Anything, mock.Anything).
			Return(&targetagentproto.ListFilesResponse{
				Responses: []*targetagentproto.FileInfos{
					{FileInfos: []*targetagentproto.FileInfo{
						{Path: "/remote/timeline/series_id=*/bin_duration=*/counter.parquet", Error: os.ErrNotExist.Error()},
					}},
				},
			}, nil).Once()

		stage := &RetrieveAgentFilesStage{
			RecipeCollector: &recipe.Collector{
				FileRetriever: &recipe.RetrieveAgentFilesStageRetriever{
					FileTransfers: []recipe.TransferRequest{
						{
							FileTransfer: conductor.FileTransfer{
								LocalPath:  filepath.Join(t.TempDir(), "out", "series_id=*", "bin_duration=*", "counter.parquet"),
								RemotePath: "/remote/timeline/series_id=*/bin_duration=*/counter.parquet",
							},
							AgentSupplier: func() *agent.AgentConn { return &agent.AgentConn{Client: tAgent} },
						},
					},
				},
			},
		}
		stageCtx := &recipe.StageContext{
			Context: context.Background(),
			CommandStateChannel: &cmdsync.CommandStateChannel{
				StopChan: make(chan struct{}),
			},
		}

		_, err := stage.Execute(stageCtx)
		require.NoError(t, err)
		tAgent.AssertExpectations(t)
	})

	t.Run("concrete remote missing with globbed local is reported as an error", func(t *testing.T) {
		remotePath := "/remote/timeline/counter.parquet"

		tAgent := &targetagentmocks.TargetAgentClient{}
		tAgent.On("ListFiles", mock.Anything, mock.Anything).
			Return(&targetagentproto.ListFilesResponse{
				Responses: []*targetagentproto.FileInfos{
					{FileInfos: []*targetagentproto.FileInfo{
						{Path: remotePath, Error: os.ErrNotExist.Error()},
					}},
				},
			}, nil).Once()

		stage := &RetrieveAgentFilesStage{
			RecipeCollector: &recipe.Collector{
				FileRetriever: &recipe.RetrieveAgentFilesStageRetriever{
					FileTransfers: []recipe.TransferRequest{
						{
							FileTransfer: conductor.FileTransfer{
								LocalPath:  filepath.Join(t.TempDir(), "out", "series_id=*", "counter.parquet"),
								RemotePath: remotePath,
							},
							AgentSupplier: func() *agent.AgentConn { return &agent.AgentConn{Client: tAgent} },
						},
					},
				},
			},
		}
		stageCtx := &recipe.StageContext{
			Context: context.Background(),
			CommandStateChannel: &cmdsync.CommandStateChannel{
				StopChan: make(chan struct{}),
			},
		}

		_, err := stage.Execute(stageCtx)
		expectedMetadata := map[string]string{
			"filePath": remotePath,
			"error":    fmt.Sprintf("file does not exist on target: %q", os.ErrNotExist.Error()),
		}
		expectedErr := message.New(message.EngineRecipeStagesRetrieveAgentFilesRetrieveFile).WithMetadata(expectedMetadata)
		require.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		tAgent.AssertExpectations(t)
	})

	t.Run("Stopped run transfers all files", func(t *testing.T) {
		s := newTestStage(t, true)

		close(s.stageCtx.CommandStateChannel.StopChan)

		_, err := s.stage.Execute(s.stageCtx)
		require.NoError(t, err)

		data, readErr := os.ReadFile(s.logPath)
		require.NoError(t, readErr)
		assert.Equal(t, s.logContents, string(data))

		data, readErr = os.ReadFile(s.dataPath)
		require.NoError(t, readErr)
		assert.Equal(t, s.dataContents, string(data))

		assert.Equal(t, s.notifier.received.Load(), int64(len(s.logContents)+len(s.dataContents)))

		s.tAgent.AssertExpectations(t)
		s.logStream.AssertExpectations(t)
		s.dataStream.AssertExpectations(t)
	})

	t.Run("Cancelled run transfers no files", func(t *testing.T) {
		s := newTestStage(t, false)

		close(s.stageCtx.CommandStateChannel.CancelChan)

		_, err := s.stage.Execute(s.stageCtx)
		expectedErr := message.New(message.EngineCommonUserCancellationError)
		require.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))

		_, readErr := os.ReadFile(s.logPath)
		require.Error(t, readErr)

		_, readErr = os.ReadFile(s.dataPath)
		require.Error(t, readErr)

		assert.Equal(t, s.notifier.received.Load(), int64(0))
	})
}

func TestRetrieveAgentFilesStage_ErrorType(t *testing.T) {
	assert.Equal(t, run.RecipeFailureRetrieve, (&RetrieveAgentFilesStage{}).ErrorType())
}

func TestRetrieveAgentFilesStage_AlwaysRun(t *testing.T) {
	assert.True(t, (&RetrieveAgentFilesStage{}).AlwaysExecute())
}
