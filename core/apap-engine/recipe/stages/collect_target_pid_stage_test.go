// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	targetagentmocks "github.com/Arm-Debug/apap-cli/api/mocks"
	"github.com/Arm-Debug/apap-cli/api/targetagentproto"
)

type pidTestRunWriter struct {
	manifestWrites  int
	entityDirWrites int
}

func (w *pidTestRunWriter) WriteManifest(run.RunBuilder) error {
	w.manifestWrites++
	return nil
}

func (w *pidTestRunWriter) WriteEntityDirs(run.RunBuilder) error {
	w.entityDirWrites++
	return nil
}

func TestTargetPIDCollection_agent(t *testing.T) {
	t.Run("missing manifest updater", func(t *testing.T) {
		stage := NewCollectTargetPIDStage(nil, afero.NewMemMapFs(), nil)

		_, err := stage.Execute(&recipe.StageContext{Context: context.Background()})

		assert.ErrorIs(t, err, message.New(message.CommonUnknownError))
	})

	t.Run("test successful pid collection creates output files", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		agentSupplier := func() *agent.AgentConn { return &agent.AgentConn{Client: client} }

		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(filepath.Dir(targetPIDComponentRelativePath()), 0o755))
		builder := run.RunBuilder{}
		writer := &pidTestRunWriter{}
		updater := run.NewRunManifestUpdater(&builder, writer)
		stage := NewCollectTargetPIDStage(agentSupplier, fs, updater)
		client.On("ListProcesses", mock.Anything, mock.Anything).Return(&targetagentproto.ProcessList{
			Processes: []*targetagentproto.ProcessInfo{
				{Pid: 123, Name: "proc1", User: "root", CommandLine: "proc1 -arg1 -arg2"},
				{Pid: 456, Name: "proc2", User: "alice", CommandLine: "proc2 -arg3 -arg4"}},
		}, nil)

		// Execute returns no errors
		_, err := stage.Execute(&recipe.StageContext{Context: context.Background()})
		assert.NoError(t, err)

		// Output files exists and contain what we expect
		os := []byte(`{"processes":[{"pid":123,"name":"proc1","username":"root","command_line":"proc1 -arg1 -arg2"},{"pid":456,"name":"proc2","username":"alice","command_line":"proc2 -arg3 -arg4"}]}`)
		exists, _ := afero.FileContainsBytes(fs, targetPIDComponentRelativePath(), os)
		assert.True(t, exists)

		assert.Equal(t, 1, builder.ComponentCount())
		assert.Equal(t, 2, writer.manifestWrites)
		assert.Equal(t, 1, writer.entityDirWrites)
		assert.Equal(t, "1.0", targetPIDOutputs.ComponentType.SchemaVersion)
		assert.Equal(t, PIDComponentNamePrefix+"-pids", targetPIDOutputs.ComponentType.Name)
	})

	t.Run("test PID write failure", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		agentSupplier := func() *agent.AgentConn { return &agent.AgentConn{Client: client} }

		fs := afero.NewReadOnlyFs(afero.NewMemMapFs())
		builder := run.RunBuilder{}
		updater := run.NewRunManifestUpdater(&builder, &pidTestRunWriter{})
		stage := NewCollectTargetPIDStage(agentSupplier, fs, updater)
		client.On("ListProcesses", mock.Anything, mock.Anything).Return(&targetagentproto.ProcessList{
			Processes: []*targetagentproto.ProcessInfo{
				{Pid: 123, Name: "proc1", User: "root", CommandLine: "proc1 -arg1 -arg2"},
				{Pid: 456, Name: "proc2", User: "alice", CommandLine: "proc2 -arg3 -arg4"}},
		}, nil)

		// Execute reports the write failure.
		_, err := stage.Execute(&recipe.StageContext{Context: context.Background()})
		assert.ErrorIs(t, err, message.New(message.EngineRecipeStagesCollectTargetPidStageWritePidFile))
	})

	t.Run("test pid collection agent error propagated", func(t *testing.T) {
		// Setup
		client := &targetagentmocks.TargetAgentClient{}

		agentSupplier := func() *agent.AgentConn {
			return &agent.AgentConn{Client: client}
		}
		fs := afero.NewMemMapFs()
		builder := run.RunBuilder{}
		updater := run.NewRunManifestUpdater(&builder, &pidTestRunWriter{})

		stage := NewCollectTargetPIDStage(agentSupplier, fs, updater)
		client.On("ListProcesses", mock.Anything, mock.Anything).Return(&targetagentproto.ProcessList{}, errors.New("boom!"))

		// Execute reports the agent error.
		_, err := stage.Execute(&recipe.StageContext{Context: context.Background()})

		// Verify
		assert.Equal(t, errors.New("boom!"), err)
	})

	t.Run("unsupported process collection is treated as unavailable", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		agentSupplier := func() *agent.AgentConn { return &agent.AgentConn{Client: client} }
		fs := afero.NewMemMapFs()
		builder := run.RunBuilder{}
		writer := &pidTestRunWriter{}
		updater := run.NewRunManifestUpdater(&builder, writer)
		stage := NewCollectTargetPIDStage(agentSupplier, fs, updater)
		unsupportedErr := message.New(message.AgentSystemInfoUnsupportedPlatform).
			WithMetadata(map[string]string{"platform": "android"})
		client.On("ListProcesses", mock.Anything, mock.Anything).
			Return(&targetagentproto.ProcessList{}, unsupportedErr)
		ctx := &recipe.StageContext{Context: context.Background()}

		_, err := stage.Execute(ctx)

		assert.NoError(t, err)
		assert.ErrorIs(t, ctx.CachedAgentProcessListErr, unsupportedErr)
		assert.Equal(t, 0, builder.ComponentCount())
		assert.Equal(t, 0, writer.manifestWrites)
		assert.Equal(t, 0, writer.entityDirWrites)
		exists, fsErr := afero.Exists(fs, targetPIDComponentRelativePath())
		assert.NoError(t, fsErr)
		assert.False(t, exists)
	})
}
