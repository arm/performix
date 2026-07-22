// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	targetsessionmocks "github.com/Arm-Debug/apap-cli/apap-engine/targetsession/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/clients/go/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

func TestRunExecutionContext_TargetInfo_NilSupplier(t *testing.T) {
	ctx := &RunExecutionContext{}
	require.Nil(t, ctx.TargetInfo())
}

func TestRunExecutionContextCopyFileRejectsUnsupportedLocalities(t *testing.T) {
	tests := []struct {
		name                string
		sourceLocality      string
		destinationLocality string
		wantError           string
	}{
		{name: "host to host", sourceLocality: "host", destinationLocality: "host", wantError: `unsupported source locality "host"`},
		{name: "target to target", sourceLocality: "target", destinationLocality: "target", wantError: `unsupported destination locality "target"`},
		{name: "host to target", sourceLocality: "host", destinationLocality: "target", wantError: `unsupported source locality "host"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &RunExecutionContext{}
			err := ctx.copyFile(tt.sourceLocality, tt.destinationLocality, "/remote/file", "/local/file")
			require.ErrorContains(t, err, tt.wantError)
		})
	}
}

func TestRunExecutionContextCopyFileRequiresTransferManager(t *testing.T) {
	ctx := &RunExecutionContext{
		Collector: &Collector{FileRetriever: &RetrieveAgentFilesStageRetriever{}},
	}

	err := ctx.copyFile("target", "host", "/remote/file", "/local/file")

	require.ErrorContains(t, err, "copyFrom requires APXD_ENABLE_TRANSFER_MANAGER=true")
}

func TestCollectMonitorTargets(t *testing.T) {
	intCtxs := []tool.IntegrationContext{
		{Workload: &tool.WorkloadAttach{PID: 123}},
		{Workload: &tool.WorkloadSystemWide{}},
	}

	targets := collectMonitorTargets(intCtxs)
	require.NotNil(t, targets)
	require.Contains(t, targets.pids, int32(123))

	require.Nil(t, collectMonitorTargets([]tool.IntegrationContext{
		{Workload: &tool.WorkloadSystemWide{}},
	}))
}

func TestStartWorkloadMonitorStopsOnPidExit(t *testing.T) {
	t.Cleanup(func() { workloadMonitorPollInterval = time.Second })
	workloadMonitorPollInterval = 10 * time.Millisecond

	mockClient := &mocks.TargetAgentClient{}
	firstResp := &targetagentproto.ProcessList{
		Processes: []*targetagentproto.ProcessInfo{{Pid: 456, CommandLine: "/bin/app"}},
	}
	secondResp := &targetagentproto.ProcessList{}
	mockClient.On("ListProcesses", mock.Anything, mock.Anything).Return(firstResp, nil).Once()
	mockClient.On("ListProcesses", mock.Anything, mock.Anything).Return(secondResp, nil).Once()

	targets := &workloadMonitorTargets{
		pids: map[int32]struct{}{456: {}},
	}

	stopCh, cancel, done := startWorkloadMonitor(context.Background(), mockClient, targets)
	require.NotNil(t, stopCh)
	require.NotNil(t, cancel)
	require.NotNil(t, done)

	select {
	case <-stopCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected stop channel to close when pid disappeared")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("monitor goroutine did not exit")
	}

	mockClient.AssertExpectations(t)
}

func TestMergeStopChannels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	primary := make(chan struct{})
	secondary := make(chan struct{})

	merged := mergeStopChannels(ctx, primary, secondary)

	// Ensure blocking until one closes.
	select {
	case <-merged:
		t.Fatal("merged channel closed prematurely")
	case <-time.After(10 * time.Millisecond):
	}

	// Closing primary should close merged.
	close(primary)
	select {
	case <-merged:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("expected merged channel to close when primary closes")
	}

	// With no secondary channel provided, merged should be the same as primary.
	primaryOnly := make(chan struct{})
	result := mergeStopChannels(ctx, primaryOnly, nil)
	if result != primaryOnly {
		t.Fatal("expected mergeStopChannels to return primary when secondary is nil")
	}
}

func TestComposeCleanup(t *testing.T) {
	var originalCalled bool
	var cancelled bool
	done := make(chan struct{})

	original := func() {
		originalCalled = true
	}
	cancel := func() {
		cancelled = true
	}

	closure := composeCleanup(original, cancel, done)

	// Should block until done is closed.
	go func() {
		time.Sleep(10 * time.Millisecond)
		close(done)
	}()

	closure()

	if !cancelled {
		t.Fatal("expected cancel to be called")
	}
	if !originalCalled {
		t.Fatal("expected original cleanup to be called")
	}
}

func TestHostLocalityCreatesHostEngine(t *testing.T) {
	targetPlatform := &conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}}
	targetAgentConn := agent.NewAgentConn(&mocks.TargetAgentClient{})
	t.Cleanup(targetAgentConn.Abort)
	hostClient := &mocks.TargetAgentClient{}
	agentConn := agent.NewAgentConn(hostClient)
	t.Cleanup(agentConn.Abort)
	hostClient.On("ExecCommand", mock.Anything, mock.MatchedBy(func(req *targetagentproto.ExecCommandRequest) bool {
		return req != nil && len(req.Command) == 1 && req.Command[0] == "/bin/true"
	})).Return(&targetagentproto.CommandResult{Rc: 0}, nil).Once()
	session := &targetsessionmocks.MockTargetSession{}
	session.On(
		"Connect",
		mock.Anything,
		targetsession.ConnectOptions{PlatformGate: conductor.HostSupported},
	).Return(nil, nil).Once()
	session.On("TargetPlatform").Return(targetPlatform, nil).Once()
	session.On("TargetAgent", mock.Anything).Return(agentConn, nil).Once()
	session.On("ResolveToolsDir").Return("/host/tools").Once()
	provider := &targetsessionmocks.MockTargetSessionProvider{}
	provider.On("TargetSession", mock.Anything).Return(session, nil).Once()

	execCtx := &RunExecutionContext{
		TargetSessions: provider,
		AgentSupplier: func() *agent.AgentConn {
			return targetAgentConn
		},
		StageNotifier:     &NullStageNotifier{},
		RecipeCtx:         &RecipeCtx{},
		RootWorkerEnabled: false,
		UsrMessageWriter:  nil,
		ToolPathsSupplier: func() deployer.BaseToolDeploymentPaths {
			return deployer.BaseToolDeploymentPaths{DeployedToolsDirectory: "/target/tools"}
		},
		TargetPlatform: func() *conductor.TargetPlatform { return targetPlatform },
	}
	runCtx := context.Background()
	_, _, localityResolver, cleanup := execCtx.newEngineLocalities(runCtx, false)
	defer cleanup()

	hostLocality, err := localityResolver("host")
	require.NoError(t, err)
	require.Equal(t, "/host/tools", hostLocality.ToolsRoot)

	_, err = hostLocality.Engine.ExecCommand(&process.LaunchCommand{Command: []string{"/bin/true"}})
	require.NoError(t, err)

	hostClient.AssertExpectations(t)
	session.AssertExpectations(t)
	provider.AssertExpectations(t)
}

func TestHostLocalityCreationErrors(t *testing.T) {
	t.Run("propagates target session lookup failure", func(t *testing.T) {
		targetPlatform := &conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}}
		targetAgentConn := agent.NewAgentConn(&mocks.TargetAgentClient{})
		t.Cleanup(targetAgentConn.Abort)
		expectedErr := errors.New("target session lookup failed")
		provider := &targetsessionmocks.MockTargetSessionProvider{}
		provider.On("TargetSession", mock.Anything).Return(&targetsessionmocks.MockTargetSession{}, expectedErr).Once()

		execCtx := &RunExecutionContext{
			TargetSessions: provider,
			AgentSupplier: func() *agent.AgentConn {
				return targetAgentConn
			},
			StageNotifier:     &NullStageNotifier{},
			RecipeCtx:         &RecipeCtx{},
			RootWorkerEnabled: false,
			UsrMessageWriter:  nil,
			ToolPathsSupplier: func() deployer.BaseToolDeploymentPaths {
				return deployer.BaseToolDeploymentPaths{DeployedToolsDirectory: "/target/tools"}
			},
			TargetPlatform: func() *conductor.TargetPlatform { return targetPlatform },
		}
		_, _, localityResolver, cleanup := execCtx.newEngineLocalities(context.Background(), false)
		defer cleanup()

		_, err := localityResolver("host")
		require.ErrorIs(t, err, expectedErr)
		provider.AssertExpectations(t)
	})

	t.Run("propagates host session connect failure", func(t *testing.T) {
		targetPlatform := &conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}}
		targetAgentConn := agent.NewAgentConn(&mocks.TargetAgentClient{})
		t.Cleanup(targetAgentConn.Abort)
		expectedErr := errors.New("host connect failed")
		session := &targetsessionmocks.MockTargetSession{}
		session.On(
			"Connect",
			mock.Anything,
			targetsession.ConnectOptions{PlatformGate: conductor.HostSupported},
		).Return(nil, expectedErr).Once()
		provider := &targetsessionmocks.MockTargetSessionProvider{}
		provider.On("TargetSession", mock.Anything).Return(session, nil).Once()

		execCtx := &RunExecutionContext{
			TargetSessions: provider,
			AgentSupplier: func() *agent.AgentConn {
				return targetAgentConn
			},
			StageNotifier:     &NullStageNotifier{},
			RecipeCtx:         &RecipeCtx{},
			RootWorkerEnabled: false,
			UsrMessageWriter:  nil,
			ToolPathsSupplier: func() deployer.BaseToolDeploymentPaths {
				return deployer.BaseToolDeploymentPaths{DeployedToolsDirectory: "/target/tools"}
			},
			TargetPlatform: func() *conductor.TargetPlatform { return targetPlatform },
		}
		_, _, localityResolver, cleanup := execCtx.newEngineLocalities(context.Background(), false)
		defer cleanup()

		_, err := localityResolver("host")
		require.ErrorIs(t, err, expectedErr)
		session.AssertExpectations(t)
		provider.AssertExpectations(t)
	})

	t.Run("propagates host agent creation failure", func(t *testing.T) {
		targetPlatform := &conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}}
		targetAgentConn := agent.NewAgentConn(&mocks.TargetAgentClient{})
		t.Cleanup(targetAgentConn.Abort)
		expectedErr := errors.New("host agent failed")
		session := &targetsessionmocks.MockTargetSession{}
		session.On(
			"Connect",
			mock.Anything,
			targetsession.ConnectOptions{PlatformGate: conductor.HostSupported},
		).Return(nil, nil).Once()
		session.On("TargetPlatform").Return(targetPlatform, nil).Once()
		session.On("TargetAgent", mock.Anything).Return(&agent.AgentConn{}, expectedErr).Once()
		provider := &targetsessionmocks.MockTargetSessionProvider{}
		provider.On("TargetSession", mock.Anything).Return(session, nil).Once()

		execCtx := &RunExecutionContext{
			TargetSessions: provider,
			AgentSupplier: func() *agent.AgentConn {
				return targetAgentConn
			},
			StageNotifier:     &NullStageNotifier{},
			RecipeCtx:         &RecipeCtx{},
			RootWorkerEnabled: false,
			UsrMessageWriter:  nil,
			ToolPathsSupplier: func() deployer.BaseToolDeploymentPaths {
				return deployer.BaseToolDeploymentPaths{DeployedToolsDirectory: "/target/tools"}
			},
			TargetPlatform: func() *conductor.TargetPlatform { return targetPlatform },
		}
		_, _, localityResolver, cleanup := execCtx.newEngineLocalities(context.Background(), false)
		defer cleanup()

		_, err := localityResolver("host")
		require.ErrorIs(t, err, expectedErr)
		session.AssertExpectations(t)
		provider.AssertExpectations(t)
	})
}

func TestContextCancelledOnHostAgentDisconnect(t *testing.T) {
	targetPlatform := &conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}}
	targetAgentConn := agent.NewAgentConn(&mocks.TargetAgentClient{})
	t.Cleanup(targetAgentConn.Abort)
	hostClient := &mocks.TargetAgentClient{}
	hostAgentConn := agent.NewAgentConn(hostClient)
	t.Cleanup(hostAgentConn.Abort)

	session := &targetsessionmocks.MockTargetSession{}
	session.On(
		"Connect",
		mock.Anything,
		targetsession.ConnectOptions{PlatformGate: conductor.HostSupported},
	).Return(nil, nil).Once()
	session.On("TargetPlatform").Return(targetPlatform, nil).Once()
	session.On("TargetAgent", mock.Anything).Return(hostAgentConn, nil).Once()
	session.On("ResolveToolsDir").Return("/host/tools").Once()
	provider := &targetsessionmocks.MockTargetSessionProvider{}
	provider.On("TargetSession", mock.Anything).Return(session, nil).Once()

	execCtx := &RunExecutionContext{
		TargetSessions: provider,
		AgentSupplier: func() *agent.AgentConn {
			return targetAgentConn
		},
		StageNotifier:     &NullStageNotifier{},
		RecipeCtx:         &RecipeCtx{},
		RootWorkerEnabled: false,
		UsrMessageWriter:  nil,
		ToolPathsSupplier: func() deployer.BaseToolDeploymentPaths {
			return deployer.BaseToolDeploymentPaths{DeployedToolsDirectory: "/target/tools"}
		},
		TargetPlatform: func() *conductor.TargetPlatform { return targetPlatform },
	}
	runCtx := context.Background()
	sharedExecCtx, _, localityResolver, cleanup := execCtx.newEngineLocalities(runCtx, false)
	defer cleanup()

	_, err := localityResolver("host")
	require.NoError(t, err)

	hostAgentConn.Abort()

	select {
	case <-sharedExecCtx.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for target locality context cancellation")
	}

	require.ErrorIs(t, context.Cause(sharedExecCtx), agent.ErrAgentDisconnected)
	session.AssertExpectations(t)
	provider.AssertExpectations(t)
}

func TestHostLocalityCleanupSurvivesTargetDisconnect(t *testing.T) {
	targetPlatform := &conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}}
	targetAgentConn := agent.NewAgentConn(&mocks.TargetAgentClient{})
	t.Cleanup(targetAgentConn.Abort)
	hostClient := &mocks.TargetAgentClient{}
	hostAgentConn := agent.NewAgentConn(hostClient)
	t.Cleanup(hostAgentConn.Abort)

	session := &targetsessionmocks.MockTargetSession{}
	session.On(
		"Connect",
		mock.Anything,
		targetsession.ConnectOptions{PlatformGate: conductor.HostSupported},
	).Return(nil, nil).Once()
	session.On("TargetPlatform").Return(targetPlatform, nil).Once()
	session.On("TargetAgent", mock.Anything).Return(hostAgentConn, nil).Once()
	session.On("ResolveToolsDir").Return("/host/tools").Once()
	provider := &targetsessionmocks.MockTargetSessionProvider{}
	provider.On("TargetSession", mock.Anything).Return(session, nil).Once()

	hostClient.On("StartProcess", mock.Anything, mock.MatchedBy(func(req *targetagentproto.StartProcessRequest) bool {
		return req != nil && len(req.Command) == 1 && req.Command[0] == "host-helper"
	})).
		Return(&targetagentproto.StartProcessResponse{Pid: 123}, nil).Once()
	hostClient.On(
		"ReleaseProcessHandles",
		mock.Anything,
		&targetagentproto.ReleaseProcessHandlesRequest{Pids: []int32{123}},
	).Return(&emptypb.Empty{}, nil).Once()

	execCtx := &RunExecutionContext{
		TargetSessions: provider,
		AgentSupplier: func() *agent.AgentConn {
			return targetAgentConn
		},
		StageNotifier:     &NullStageNotifier{},
		RecipeCtx:         &RecipeCtx{},
		RootWorkerEnabled: false,
		UsrMessageWriter:  nil,
		ToolPathsSupplier: func() deployer.BaseToolDeploymentPaths {
			return deployer.BaseToolDeploymentPaths{DeployedToolsDirectory: "/target/tools"}
		},
		TargetPlatform: func() *conductor.TargetPlatform { return targetPlatform },
	}
	runCtx := context.Background()
	_, _, localityResolver, cleanup := execCtx.newEngineLocalities(runCtx, false)

	hostLocality, err := localityResolver("host")
	require.NoError(t, err)

	_, err = hostLocality.Engine.StartProcess(&process.StartProcess{
		LaunchCommand: process.LaunchCommand{Command: []string{"host-helper"}},
	})
	require.NoError(t, err)

	targetAgentConn.Abort()
	cleanup()

	hostClient.AssertExpectations(t)
	session.AssertExpectations(t)
	provider.AssertExpectations(t)
}
