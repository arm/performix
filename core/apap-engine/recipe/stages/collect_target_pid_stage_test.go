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

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	targetagentmocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

func TestTargetPIDCollection_agent(t *testing.T) {
	t.Run("test successful pid collection creates output files", func(t *testing.T) {
		var output util.Named[recipe.CollectorOutput]
		client := &targetagentmocks.TargetAgentClient{}
		sink := func(o util.Named[recipe.CollectorOutput]) { output = o }
		outputPathSupplier := func() string { return "/foo/pids" }
		agentSupplier := func() *agent.AgentConn { return &agent.AgentConn{Client: client} }

		fs := afero.NewMemMapFs()
		stage := NewCollectTargetPIDStage(outputPathSupplier, agentSupplier, sink, fs)
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
		exists, _ := afero.FileContainsBytes(fs, outputPathSupplier(), os)
		assert.True(t, exists)

		// Confing schema and name match - these are currently the same for all SLCollectorTargetInfo output files
		assert.Equal(t, output.Value.ComponentType.SchemaVersion, "1.0")
		assert.Equal(t, output.Value.ComponentType.Name, PIDComponentNamePrefix+"-pids")
	})

	t.Run("test PID write failure", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		sink := func(o util.Named[recipe.CollectorOutput]) {}
		outputPath := filepath.Join(t.TempDir(), "nonextistentdir", "pids")
		outputPathSupplier := func() string { return outputPath }
		agentSupplier := func() *agent.AgentConn { return &agent.AgentConn{Client: client} }

		fs := afero.NewOsFs()
		stage := NewCollectTargetPIDStage(outputPathSupplier, agentSupplier, sink, fs)
		client.On("ListProcesses", mock.Anything, mock.Anything).Return(&targetagentproto.ProcessList{
			Processes: []*targetagentproto.ProcessInfo{
				{Pid: 123, Name: "proc1", User: "root", CommandLine: "proc1 -arg1 -arg2"},
				{Pid: 456, Name: "proc2", User: "alice", CommandLine: "proc2 -arg3 -arg4"}},
		}, nil)

		// Execute returns no errors
		_, err := stage.Execute(&recipe.StageContext{Context: context.Background()})
		assert.ErrorIs(t, err, message.New(message.EngineRecipeStagesCollectTargetPidStageWritePidFile))
	})

	t.Run("test pid collection agent error propagated", func(t *testing.T) {
		// Setup
		client := &targetagentmocks.TargetAgentClient{}
		sink := func(o util.Named[recipe.CollectorOutput]) {}
		outputPathSupplier := func() string { return "/foo/pids" }

		agentSupplier := func() *agent.AgentConn {
			return &agent.AgentConn{Client: client}
		}
		fs := afero.NewMemMapFs()

		stage := NewCollectTargetPIDStage(outputPathSupplier, agentSupplier, sink, fs)
		client.On("ListProcesses", mock.Anything, mock.Anything).Return(&targetagentproto.ProcessList{}, errors.New("boom!"))

		// Execute returns no errors
		_, err := stage.Execute(&recipe.StageContext{Context: context.Background()})

		// Verify
		assert.Equal(t, errors.New("boom!"), err)
	})
}
