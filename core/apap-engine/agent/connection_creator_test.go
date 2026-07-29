// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	conductormocks "github.com/Arm-Debug/apap-cli/apap-engine/conductor/conductormocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcconnection"
	"github.com/Arm-Debug/apap-cli/apap-engine/locality"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
	targetagentmocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
	"github.com/Arm-Debug/apap-cli/clients/go/tetherproto"
)

func TestGetAgentPath(t *testing.T) {
	base := "/tmp/tools"
	cmdRunner := &conductormocks.MockCommandRunner{}
	cmdRunner.On("RunCommand", "test -e /tmp/tools").Return("", "", nil).Once()
	cmdRunner.On("RunCommand", "test -w /tmp/tools").Return("", "", nil).Once()

	tp := conductor.TargetPlatform{
		PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Linux},
		Path:                  &conductor.LinuxPathUtils{},
		Actions:               &conductormocks.MockTargetActions{CmdRunner: cmdRunner},
	}
	got := GetAgentPath(base, tp, locality.Target)
	want := fmt.Sprintf("%v/%v/%v/%v/%v", base, terminology.GetProductBinaryName(), "tools", terminology.GetAgentBinaryName(), versions.GetVersion())
	assert.Equal(t, want, got)
	cmdRunner.AssertExpectations(t)
}

func TestGetAgentPath_WindowsDefault(t *testing.T) {
	base := ""

	cmdRunner := &conductormocks.MockCommandRunner{}
	localAppData := "C:/Users/runner/AppData/Local"
	cmdRunner.On("RunCommand", `powershell -NoLogo -WindowStyle Hidden -Command "echo $env:LOCALAPPDATA"`).Return("C:\\Users\\runner\\AppData\\Local\r\n", "", nil).Once()
	cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoLogo -WindowStyle Hidden -Command "if (Test-Path '%s') { exit 0 } else { exit 1 }"`, localAppData)).Return("", "", nil).Once()
	cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoLogo -WindowStyle Hidden -Command "$p = '%s'; $tmp = Join-Path $p '%v-writecheck.tmp'; try { Set-Content -Path $tmp -Value '' -ErrorAction Stop; Remove-Item $tmp -Force; exit 0 } catch { exit 1 }"`, localAppData, terminology.GetProductBinaryName())).
		Return("", "", nil).Once()

	tp := conductor.TargetPlatform{
		PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Win},
		Path:                  &conductor.WindowsPathUtils{},
		Actions:               &conductormocks.MockTargetActions{CmdRunner: cmdRunner},
	}

	got := GetAgentPath(base, tp, locality.Target)
	expectedBase := filepath.Join("C:/Users/runner/AppData/Local", terminology.GetProductBinaryName(), "tools")
	want := fmt.Sprintf(`%v\%v\%v`, tp.Path.ToOSPath(expectedBase), terminology.GetAgentBinaryName(), versions.GetVersion())
	assert.Equal(t, want, got)
	cmdRunner.AssertExpectations(t)
}

func TestGetAgentPath_WindowsConfiguredPathNoActions(t *testing.T) {
	base := "C:/tools"

	cmdRunner := &conductormocks.MockCommandRunner{}
	cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoLogo -WindowStyle Hidden -Command "if (Test-Path '%s') { exit 0 } else { exit 1 }"`, base)).Return("", "", nil).Once()
	cmdRunner.On("RunCommand", fmt.Sprintf(`powershell -NoLogo -WindowStyle Hidden -Command "$p = '%s'; $tmp = Join-Path $p '%v-writecheck.tmp'; try { Set-Content -Path $tmp -Value '' -ErrorAction Stop; Remove-Item $tmp -Force; exit 0 } catch { exit 1 }"`, base, terminology.GetProductBinaryName())).
		Return("", "", nil).Once()

	tp := conductor.TargetPlatform{
		PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Win},
		Path:                  &conductor.WindowsPathUtils{},
		Actions:               &conductormocks.MockTargetActions{CmdRunner: cmdRunner},
	}

	got := GetAgentPath(base, tp, locality.Target)
	expectedBase := filepath.Join("C:/tools", terminology.GetProductBinaryName(), "tools")
	want := fmt.Sprintf(`%v\%v\%v`, tp.Path.ToOSPath(expectedBase), terminology.GetAgentBinaryName(), versions.GetVersion())
	assert.Equal(t, want, got)
	cmdRunner.AssertExpectations(t)
}

func TestGetAgentPath_ResolverErrorReturnsEmpty(t *testing.T) {
	cmdRunner := &conductormocks.MockCommandRunner{}
	cmdRunner.On("RunCommand", "printenv HOME").Return("", "", nil).Once()

	tp := conductor.TargetPlatform{
		PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Linux},
		Path:                  &conductor.LinuxPathUtils{},
		Actions:               &conductormocks.MockTargetActions{CmdRunner: cmdRunner},
	}

	got := GetAgentPath("", tp, locality.Target)
	assert.Empty(t, got)
	cmdRunner.AssertExpectations(t)
}

func TestNewAgentConnection_Success(t *testing.T) {
	mc := &grpcconnection.GRPCConnectorMock{}
	mc.On("Connect", "127.0.0.1", 4242, defaultConnectionTimeout).
		Return((*grpc.ClientConn)(nil), nil).
		Once()

	client, cc, err := newAgentConnection(mc, 4242, locality.Target)
	assert.NoError(t, err)
	assert.NotNil(t, client)
	assert.Nil(t, cc)
	mc.AssertExpectations(t)
}

func TestNewAgentConnection_Fail(t *testing.T) {
	mc := &grpcconnection.GRPCConnectorMock{}
	mc.On("Connect", "127.0.0.1", 7777, defaultConnectionTimeout).
		Return((*grpc.ClientConn)(nil), errors.New("connect failed")).
		Once()

	_, _, err := newAgentConnection(mc, 7777, locality.Target)
	expectedMetadata := map[string]string{"portNum": "7777", "locality": locality.Target}
	expectedErr := message.New(message.EngineAgentConnectionCreatorNewAgentConnection).WithCause(errors.New("connect failed")).WithMetadata(expectedMetadata)
	assert.Equal(t, expectedErr, err)
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	mc.AssertExpectations(t)
}

func TestAgentConnClose_Fail(t *testing.T) {
	mc := &targetagentmocks.TargetAgentClient{}
	mc.On("Shutdown", mock.Anything, mock.Anything).
		Return(&emptypb.Empty{}, errors.New("shutdown fail")).Once()

	ac := &AgentConn{
		Client: mc,
		dataCc: nil,
	}

	err1 := ac.Close()
	assert.Error(t, err1)
	assert.ErrorContains(t, err1, "error closing agent connection:")
	assert.ErrorContains(t, err1, "grpc shutdown")

	// check second call returns same error and doesn't call closers again
	err2 := ac.Close()
	assert.Equal(t, err1, err2)

	mc.AssertExpectations(t)
}

func TestAgentConnClose_Success(t *testing.T) {
	logger := &mocks.MockAgentLogger{}
	logger.On("Close").Return().Once()

	tether := &mocks.MockAgentTether{}
	tether.On("Close").Return().Once()

	ac := &AgentConn{logger: logger, tether: tether}
	assert.NoError(t, ac.Close())
	assert.NoError(t, ac.Close()) // multiple calls succeed

	logger.AssertExpectations(t)
}

func TestCheckAgentClientConn_Success(t *testing.T) {
	mc := &targetagentmocks.TargetAgentClient{}
	mc.On("GetVersion", mock.Anything, mock.Anything).
		Return(&targetagentproto.GetVersionResponse{Version: "1.2.3"}, nil).
		Once()

	ac := &AgentConn{
		Client: mc,
	}

	err := CheckAgentClientConn(context.Background(), ac)
	assert.NoError(t, err)

	mc.AssertExpectations(t)
}

func TestCheckAgentClientConn_FailureGRPC(t *testing.T) {

	mc := &targetagentmocks.TargetAgentClient{}
	mc.On("GetVersion", mock.Anything, mock.Anything).
		Return((*targetagentproto.GetVersionResponse)(nil), errors.New("not ready")).
		Once()

	ac := &AgentConn{
		Client: mc,
	}

	err := CheckAgentClientConn(context.Background(), ac)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "health check for agent gRPC connection failed")
	assert.ErrorContains(t, err, "not ready")
	mc.AssertExpectations(t)
}

func TestLaunchAgent_Succeeds(t *testing.T) {
	t.Run("root mode (launch as admin)", func(t *testing.T) {
		actions := &conductormocks.MockTargetActions{}
		tp := conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}, Actions: actions}

		agentPath := "/tmp/tools/agent"
		expectedCmd := tp.Path.GenerateRunScriptCommand(GetAgentLaunchScript(tp), agentPath)

		actions.On("RunCommandAsAdmin", expectedCmd).Return(conductor.RunCommandOutput{
			Stdout:     "  4242 \n",
			Stderr:     "",
			ReturnCode: 0,
		}, nil).Once()

		port, err := launchAgent(tp, agentPath, false, locality.Target)
		require.NoError(t, err)
		assert.Equal(t, 4242, port)
		actions.AssertExpectations(t)
	})

	t.Run("non-root mode (launch as user)", func(t *testing.T) {
		actions := &conductormocks.MockTargetActions{}
		tp := conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}, Actions: actions}

		agentPath := "/tmp/tools/agent"
		expectedCmd := tp.Path.GenerateRunScriptCommand(GetAgentLaunchScript(tp), agentPath)

		actions.On("RunCommand", expectedCmd).Return(conductor.RunCommandOutput{
			Stdout:     "  4242 \n",
			Stderr:     "",
			ReturnCode: 0,
		}, nil).Once()

		port, err := launchAgent(tp, agentPath, true, locality.Target)
		require.NoError(t, err)
		assert.Equal(t, 4242, port)
		actions.AssertExpectations(t)
	})
}

func TestLaunchAgent_FailsRunCommandError(t *testing.T) {
	actions := &conductormocks.MockTargetActions{}
	tp := conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}, Actions: actions}

	agentPath := "/tmp/tools/agent"
	expectedCmd := tp.Path.GenerateRunScriptCommand(GetAgentLaunchScript(tp), agentPath)

	scriptErr := errors.New("script error")
	actions.On("RunCommandAsAdmin", expectedCmd).Return(conductor.RunCommandOutput{}, scriptErr).Once()
	actions.On("HasAdminPerms").Return(true, nil).Once()
	actions.On("Stat", filepath.Join(agentPath, GetAgentLaunchScript(tp))).Return(nil, nil).Once()

	_, err := launchAgent(tp, agentPath, false, locality.Target)
	expectedMetadata := map[string]string{
		"cmd":        GetAgentLaunchScript(tp),
		"workingDir": filepath.ToSlash(agentPath),
		"locality":   locality.Target,
	}
	expectedErr := message.New(message.EngineAgentConnectionCreatorRunLaunchCommand).WithCause(scriptErr).WithMetadata(expectedMetadata)
	assert.Equal(t, expectedErr, err)
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	actions.AssertExpectations(t)
}

func TestLaunchAgent_FailsRunCommandError_NonRoot(t *testing.T) {
	actions := &conductormocks.MockTargetActions{}
	tp := conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}, Actions: actions}

	agentPath := "/tmp/tools/agent"
	expectedCmd := tp.Path.GenerateRunScriptCommand(GetAgentLaunchScript(tp), agentPath)

	// Fail with some random error
	// classifyLaunchFailure() then should correctly handle the non-root mode
	// and return the appropriate error
	someErr := errors.New("some error")
	actions.On("RunCommand", expectedCmd).Return(conductor.RunCommandOutput{}, someErr).Once()
	actions.On("Stat", filepath.Join(agentPath, GetAgentLaunchScript(tp))).Return(nil, nil).Once()

	_, err := launchAgent(tp, agentPath, true, locality.Target)
	expectedMetadata := map[string]string{
		"cmd":        GetAgentLaunchScript(tp),
		"workingDir": filepath.ToSlash(agentPath),
		"locality":   locality.Target,
	}
	expectedErr := message.New(message.EngineAgentConnectionCreatorRunLaunchCommand).WithCause(someErr).WithMetadata(expectedMetadata)
	require.Equal(t, expectedErr, err)
	require.NoError(t, message.ValidateMetadataPlaceholders(err))

	// HasAdminPerms should NOT be called in non-root mode
	actions.AssertExpectations(t)
	actions.AssertNotCalled(t, "HasAdminPerms")
}

func TestLaunchAgent_FailsBadReturnCode(t *testing.T) {
	actions := &conductormocks.MockTargetActions{}
	tp := conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}, Actions: actions}

	agentPath := "/tmp/tools/agent"
	expectedCmd := tp.Path.GenerateRunScriptCommand(GetAgentLaunchScript(tp), agentPath)

	actions.On("RunCommandAsAdmin", expectedCmd).Return(conductor.RunCommandOutput{
		Stdout:     "",
		Stderr:     "script failed",
		ReturnCode: 1,
	}, nil).Once()

	actions.On("HasAdminPerms").Return(true, nil).Once()
	actions.On("Stat", filepath.Join(agentPath, GetAgentLaunchScript(tp))).Return(nil, nil).Once()

	_, err := launchAgent(tp, agentPath, false, locality.Target)
	expectedMetadata := map[string]string{
		"cmd":        GetAgentLaunchScript(tp),
		"workingDir": filepath.ToSlash(agentPath),
		"exitCode":   "1",
		"locality":   locality.Target,
	}
	expectedErr := message.New(message.EngineAgentConnectionCreatorLaunchErrorCode).WithMetadata(expectedMetadata)
	assert.Equal(t, expectedErr, err)
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	actions.AssertExpectations(t)
}

func TestLaunchAgent_FailsNoAdminPerms(t *testing.T) {
	actions := &conductormocks.MockTargetActions{}
	tp := conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}, Actions: actions}

	agentPath := "/tmp/tools/agent"
	expectedCmd := tp.Path.GenerateRunScriptCommand(GetAgentLaunchScript(tp), agentPath)

	actions.On("RunCommandAsAdmin", expectedCmd).Return(conductor.RunCommandOutput{}, errors.New("run error")).Once()
	actions.On("HasAdminPerms").Return(false, nil).Once()

	_, err := launchAgent(tp, agentPath, false, locality.Target)
	expectedErr := message.New(message.EngineAgentConnectionCreatorInsufficientPrivileges).WithMetadata(map[string]string{"locality": locality.Target})
	assert.Equal(t, expectedErr, err)
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	actions.AssertExpectations(t)
}

func TestLaunchAgent_FailsAdminCheckError(t *testing.T) {
	actions := &conductormocks.MockTargetActions{}
	tp := conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}, Actions: actions}

	agentPath := "/tmp/tools/agent"
	expectedCmd := tp.Path.GenerateRunScriptCommand(GetAgentLaunchScript(tp), agentPath)

	adminErr := errors.New("could not determine administrator or root privileges")

	actions.On("RunCommandAsAdmin", expectedCmd).Return(conductor.RunCommandOutput{}, errors.New("run error")).Once()
	actions.On("HasAdminPerms").Return(false, adminErr).Once()

	_, err := launchAgent(tp, agentPath, false, locality.Target)
	expectedErr := message.New(message.EngineAgentConnectionCreatorInsufficientPrivileges).WithMetadata(map[string]string{"locality": locality.Target}).WithCause(adminErr)
	assert.Equal(t, expectedErr, err)
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	actions.AssertExpectations(t)
}

func TestLaunchAgent_FailsScriptMissing(t *testing.T) {
	actions := &conductormocks.MockTargetActions{}
	tp := conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}, Actions: actions}

	agentPath := "/tmp/tools/agent"
	expectedCmd := tp.Path.GenerateRunScriptCommand(GetAgentLaunchScript(tp), agentPath)

	statErr := errors.New("file does not exist")

	actions.On("RunCommandAsAdmin", expectedCmd).Return(conductor.RunCommandOutput{
		Stdout:     "",
		Stderr:     "not found",
		ReturnCode: 1,
	}, nil).Once()

	actions.On("HasAdminPerms").Return(true, nil).Once()
	actions.On("Stat", filepath.Join(agentPath, GetAgentLaunchScript(tp))).Return(nil, statErr).Once()

	_, err := launchAgent(tp, agentPath, false, locality.Target)
	expectedMetadata := map[string]string{
		"cmd":        GetAgentLaunchScript(tp),
		"workingDir": filepath.ToSlash(agentPath),
		"locality":   locality.Target,
	}
	expectedErr := message.New(message.EngineAgentConnectionCreatorAgentNotDeployed).
		WithCause(statErr).
		WithMetadata(expectedMetadata)

	assert.Equal(t, expectedErr, err)
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	actions.AssertExpectations(t)
}

func TestLaunchAgent_FailsHostScriptMissing(t *testing.T) {
	actions := &conductormocks.MockTargetActions{}
	tp := conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}, Actions: actions}

	agentPath := "/tmp/tools/agent"
	expectedCmd := tp.Path.GenerateRunScriptCommand(GetAgentLaunchScript(tp), agentPath)

	statErr := errors.New("file does not exist")

	actions.On("RunCommandAsAdmin", expectedCmd).Return(conductor.RunCommandOutput{
		Stdout:     "",
		Stderr:     "not found",
		ReturnCode: 1,
	}, nil).Once()

	actions.On("HasAdminPerms").Return(true, nil).Once()
	actions.On("Stat", filepath.Join(agentPath, GetAgentLaunchScript(tp))).Return(nil, statErr).Once()

	_, err := launchAgent(tp, agentPath, false, locality.Host)
	expectedMetadata := map[string]string{
		"cmd":        GetAgentLaunchScript(tp),
		"workingDir": filepath.ToSlash(agentPath),
		"locality":   locality.Host,
	}
	expectedErr := message.New(message.EngineAgentConnectionCreatorHostAgentNotDeployed).
		WithCause(statErr).
		WithMetadata(expectedMetadata)

	assert.Equal(t, expectedErr, err)
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	actions.AssertExpectations(t)
}

func TestLaunchAgent_FailsInvalidPort(t *testing.T) {
	cases := []struct {
		name   string
		stdout string
		errMsg string
	}{
		{"non int", "abc", "non-numeric port"},
		{"zero", "0", "out-of-range port"},
		{"too big", "70000", "out-of-range port"},
		{"negative", "-1", "out-of-range port"},
		{"empty", "", "non-numeric port"},
		{"space only", "   ", "non-numeric port"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actions := &conductormocks.MockTargetActions{}
			tp := conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}, Actions: actions}

			agentPath := "/tmp/tools/agent"
			expectedCmd := tp.Path.GenerateRunScriptCommand(GetAgentLaunchScript(tp), agentPath)

			actions.On("RunCommandAsAdmin", expectedCmd).Return(conductor.RunCommandOutput{
				Stdout:     tc.stdout,
				Stderr:     "",
				ReturnCode: 0,
			}, nil).Once()

			port, err := launchAgent(tp, agentPath, false, locality.Target)
			assert.Zero(t, port)

			var msgErr message.Message
			ok := errors.As(err, &msgErr)
			assert.True(t, ok)
			assert.Equal(t, message.CommonUnknownError, msgErr.Code())
			assert.Contains(t, msgErr.Unwrap().Error(), tc.errMsg)
			actions.AssertExpectations(t)
		})
	}
}

func TestNewConnection_Success(t *testing.T) {
	client := &targetagentmocks.TargetAgentClient{}
	client.On("Shutdown", mock.Anything, mock.Anything).Return(&emptypb.Empty{}, nil).Maybe()

	tetherMock := &mocks.MockAgentTether{}
	tetherMock.On("OnTransportFailure", mock.Anything).Return().Once()
	tetherMock.On("Start").Return(nil).Once()
	tetherMock.On("Close").Return().Once()

	loggerMock := &mocks.MockAgentLogger{}
	loggerMock.On("Start").Return(nil).Once()
	loggerMock.On("Close").Return().Once()

	p := &agentConnectionCreator{
		toolsDir: "/tmp",
		launch:   func(_ conductor.TargetPlatform, _ string, _ bool, _ string) (int, error) { return 4242, nil },
		connect: func(_ grpcconnection.GRPCConnector, _ int, _ string) (targetagentproto.TargetAgentClient, *grpc.ClientConn, error) {
			return client, nil, nil
		},
		newTether: func(_ tetherproto.TetherClient, _ time.Duration) AgentTether { return tetherMock },
		newLogger: func(_ targetagentproto.TargetAgentClient, _ log.FieldLogger, _ string) AgentLogger { return loggerMock },
	}

	ac, err := p.NewConnection("target_agent", locality.Target, conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}}, nil)
	assert.NoError(t, err)
	assert.NotNil(t, ac)

	_ = ac.Close()

	tetherMock.AssertExpectations(t)
	loggerMock.AssertExpectations(t)
	client.AssertExpectations(t)
}

func TestNewConnection_RoutesDataAndControlTrafficSeparately(t *testing.T) {
	dataTargetServer := newTestTargetAgentServer()
	dataCc := newTestGRPCClientConn(t, func(server *grpc.Server) {
		targetagentproto.RegisterTargetAgentServer(server, dataTargetServer)
	})
	dataClient := targetagentproto.NewTargetAgentClient(dataCc)

	controlTargetServer := newTestTargetAgentServer()
	controlCc := newTestGRPCClientConn(t, func(server *grpc.Server) {
		targetagentproto.RegisterTargetAgentServer(server, controlTargetServer)
		tetherproto.RegisterTetherServer(server, &singlePulseTetherServer{})
	})
	controlClient := targetagentproto.NewTargetAgentClient(controlCc)

	loggerMock := &mocks.MockAgentLogger{}
	loggerMock.On("Start").Return(nil).Once()
	loggerMock.On("Close").Return().Once()

	var connectCalls int32
	p := &agentConnectionCreator{
		toolsDir: "/tmp",
		launch:   func(_ conductor.TargetPlatform, _ string, _ bool, _ string) (int, error) { return 4242, nil },
		connect: func(_ grpcconnection.GRPCConnector, _ int, _ string) (targetagentproto.TargetAgentClient, *grpc.ClientConn, error) {
			switch atomic.AddInt32(&connectCalls, 1) {
			case 1:
				return dataClient, dataCc, nil
			case 2:
				return controlClient, controlCc, nil
			default:
				t.Fatalf("unexpected connect call")
				return nil, nil, nil
			}
		},
		newTether: func(tetherClient tetherproto.TetherClient, pulseEvery time.Duration) AgentTether {
			return &testAgentTether{
				start: func() error {
					stream, err := tetherClient.Hold(context.Background())
					// dataCc does not have a tether server registered, so this tests
					// that the tetherClient is configured to use the control conn
					require.NoError(t, err)
					require.NoError(t, stream.Send(&tetherproto.PulseRequest{}))
					_, err = stream.Recv()
					require.NoError(t, err)
					return nil
				},
			}
		},
		newLogger: func(loggerClient targetagentproto.TargetAgentClient, _ log.FieldLogger, id string) AgentLogger {
			require.Same(t, controlClient, loggerClient)
			return loggerMock
		},
	}

	ac, err := p.NewConnection("target_agent", locality.Target, conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}}, nil)
	require.NoError(t, err)
	require.NotNil(t, ac)

	require.Equal(t, int32(2), atomic.LoadInt32(&connectCalls))
	require.Equal(t, controlCc, ac.controlCc)
	require.Equal(t, dataCc, ac.dataCc)
	require.Same(t, dataClient, ac.Client)

	require.NoError(t, ac.Close())
	loggerMock.AssertExpectations(t)
}

func TestAgentConnClose_ClosesDataAndControlConns(t *testing.T) {
	dataTargetServer := newTestTargetAgentServer()
	dataCc := newTestGRPCClientConn(t, func(server *grpc.Server) {
		targetagentproto.RegisterTargetAgentServer(server, dataTargetServer)
	})
	controlCc := newTestGRPCClientConn(t, nil)

	logger := &mocks.MockAgentLogger{}
	logger.On("Close").Return().Once()

	tether := &mocks.MockAgentTether{}
	tether.On("Close").Return().Once()

	ac := NewAgentConn(targetagentproto.NewTargetAgentClient(dataCc))
	ac.dataCc = dataCc
	ac.controlCc = controlCc
	ac.logger = logger
	ac.tether = tether

	require.NoError(t, ac.Close())
	require.NoError(t, ac.Close())

	require.Equal(t, int32(1), atomic.LoadInt32(&dataTargetServer.shutdownCalls))
	require.True(t, <-dataTargetServer.shutdownForce)
	require.Eventually(t, func() bool {
		return dataCc.GetState() == connectivity.Shutdown && controlCc.GetState() == connectivity.Shutdown
	}, 500*time.Millisecond, 10*time.Millisecond)

	logger.AssertExpectations(t)
	tether.AssertExpectations(t)
}

func TestAgentConnAbortWithCause_ClosesDataAndControlConns(t *testing.T) {
	dataTargetServer := newTestTargetAgentServer()
	dataCc := newTestGRPCClientConn(t, func(server *grpc.Server) {
		targetagentproto.RegisterTargetAgentServer(server, dataTargetServer)
	})
	controlCc := newTestGRPCClientConn(t, nil)

	logger := &mocks.MockAgentLogger{}
	logger.On("Close").Return().Once()

	tether := &mocks.MockAgentTether{}
	tether.On("Close").Return().Once()

	ac := NewAgentConn(targetagentproto.NewTargetAgentClient(dataCc))
	ac.dataCc = dataCc
	ac.controlCc = controlCc
	ac.logger = logger
	ac.tether = tether

	transportFailureErr := errors.New("stream recv failed")
	ac.AbortWithCause(transportFailureErr)

	require.Eventually(t, func() bool {
		return errors.Is(context.Cause(ac.AgentContext()), ErrAgentDisconnected)
	}, 500*time.Millisecond, 10*time.Millisecond)
	require.ErrorContains(t, context.Cause(ac.AgentContext()), transportFailureErr.Error())
	require.Eventually(t, func() bool {
		return dataCc.GetState() == connectivity.Shutdown && controlCc.GetState() == connectivity.Shutdown
	}, 500*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, int32(0), atomic.LoadInt32(&dataTargetServer.shutdownCalls))

	logger.AssertExpectations(t)
	tether.AssertExpectations(t)
}

func TestNewConnection_TetherError(t *testing.T) {
	tetherMock := &mocks.MockAgentTether{}
	tetherMock.On("OnTransportFailure", mock.Anything).Return().Once()
	tetherMock.On("Start").Return(assert.AnError).Once()

	// Shouldn't call Start due to tether error
	loggerMock := &mocks.MockAgentLogger{}

	client := &targetagentmocks.TargetAgentClient{}
	client.On("Shutdown", mock.Anything, mock.Anything).Return(&emptypb.Empty{}, nil).Once()

	p := &agentConnectionCreator{
		toolsDir: "/tmp",
		launch:   func(_ conductor.TargetPlatform, _ string, _ bool, _ string) (int, error) { return 4242, nil },
		connect: func(_ grpcconnection.GRPCConnector, _ int, _ string) (targetagentproto.TargetAgentClient, *grpc.ClientConn, error) {
			return client, nil, nil
		},
		newTether: func(_ tetherproto.TetherClient, _ time.Duration) AgentTether { return tetherMock },
		newLogger: func(_ targetagentproto.TargetAgentClient, _ log.FieldLogger, _ string) AgentLogger { return loggerMock },
	}

	ac, err := p.NewConnection("target_agent", locality.Target, conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}}, nil)
	assert.Equal(t, assert.AnError, err)
	assert.Nil(t, ac)

	loggerMock.AssertExpectations(t)
	tetherMock.AssertExpectations(t)
	client.AssertExpectations(t)
}

func TestNewConnection_LoggerError(t *testing.T) {
	tetherMock := &mocks.MockAgentTether{}
	tetherMock.On("OnTransportFailure", mock.Anything).Return().Once()
	tetherMock.On("Start").Return(nil).Once()
	tetherMock.On("Close").Return().Once()

	loggerMock := &mocks.MockAgentLogger{}
	loggerMock.On("Start").Return(assert.AnError).Once()

	client := &targetagentmocks.TargetAgentClient{}
	client.On("Shutdown", mock.Anything, mock.Anything).Return(&emptypb.Empty{}, nil).Once()

	p := &agentConnectionCreator{
		toolsDir: "/tmp",
		launch:   func(_ conductor.TargetPlatform, _ string, _ bool, _ string) (int, error) { return 4242, nil },
		connect: func(_ grpcconnection.GRPCConnector, _ int, _ string) (targetagentproto.TargetAgentClient, *grpc.ClientConn, error) {
			return client, nil, nil
		},
		newTether: func(_ tetherproto.TetherClient, _ time.Duration) AgentTether { return tetherMock },
		newLogger: func(_ targetagentproto.TargetAgentClient, _ log.FieldLogger, _ string) AgentLogger { return loggerMock },
	}

	ac, err := p.NewConnection("target_agent", locality.Target, conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}}, nil)
	assert.Equal(t, assert.AnError, err)
	assert.Nil(t, ac)

	tetherMock.AssertExpectations(t)
	loggerMock.AssertExpectations(t)
	client.AssertExpectations(t)
}

func TestAgentConnAbortWithCause(t *testing.T) {
	t.Run("cancels lifetime and notifies", func(t *testing.T) {
		var closeCallCount int32
		logger := &mocks.MockAgentLogger{}
		logger.On("Close").Run(func(mock.Arguments) {
			atomic.AddInt32(&closeCallCount, 1)
		}).Return().Once()

		tether := &mocks.MockAgentTether{}
		tether.On("Close").Run(func(mock.Arguments) {
			atomic.AddInt32(&closeCallCount, 1)
		}).Return().Once()

		agentConn := &AgentConn{logger: logger, tether: tether}
		agentConn.initLifetimeContext()

		connectionLostErrCh := make(chan error, 1)
		agentConn.OnConnectionLost(func(err error) {
			connectionLostErrCh <- err
		})

		transportFailureErr := errors.New("stream recv failed")
		agentConn.AbortWithCause(transportFailureErr)

		var onConnectionLostErr error
		select {
		case onConnectionLostErr = <-connectionLostErrCh:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for OnConnectionLost callback")
		}

		assert.ErrorIs(t, onConnectionLostErr, message.New(message.EngineAgentConnectionTransportError))
		assert.ErrorContains(t, onConnectionLostErr, transportFailureErr.Error())

		assert.Eventually(t, func() bool {
			return errors.Is(context.Cause(agentConn.AgentContext()), ErrAgentDisconnected)
		}, 500*time.Millisecond, 10*time.Millisecond)
		assert.ErrorContains(t, context.Cause(agentConn.AgentContext()), transportFailureErr.Error())
		assert.Eventually(t, func() bool {
			return atomic.LoadInt32(&closeCallCount) == 2
		}, 500*time.Millisecond, 10*time.Millisecond)

		logger.AssertExpectations(t)
		tether.AssertExpectations(t)
	})

	t.Run("is idempotent", func(t *testing.T) {
		var closeCallCount int32
		logger := &mocks.MockAgentLogger{}
		logger.On("Close").Run(func(mock.Arguments) {
			atomic.AddInt32(&closeCallCount, 1)
		}).Return().Once()

		tether := &mocks.MockAgentTether{}
		tether.On("Close").Run(func(mock.Arguments) {
			atomic.AddInt32(&closeCallCount, 1)
		}).Return().Once()

		agentConn := &AgentConn{logger: logger, tether: tether}
		agentConn.initLifetimeContext()

		var onConnectionLostCallCount int32
		agentConn.OnConnectionLost(func(error) {
			atomic.AddInt32(&onConnectionLostCallCount, 1)
		})

		agentConn.AbortWithCause(errors.New("first"))
		agentConn.AbortWithCause(errors.New("second"))
		agentConn.Abort()

		assert.Eventually(t, func() bool {
			return atomic.LoadInt32(&onConnectionLostCallCount) == 1
		}, 500*time.Millisecond, 10*time.Millisecond)
		assert.Eventually(t, func() bool {
			return atomic.LoadInt32(&closeCallCount) == 2
		}, 500*time.Millisecond, 10*time.Millisecond)

		logger.AssertExpectations(t)
		tether.AssertExpectations(t)
	})
}

func TestClassifyAbortCause(t *testing.T) {
	grpcUnavailableErr := status.Error(codes.Unavailable, "connection reset by peer")
	tests := []struct {
		name     string
		cause    error
		expected message.MessageCode
		metadata map[string]string
	}{
		{
			name:     "stream failure is a transport error",
			cause:    fmt.Errorf("%w: %w", errTetherStreamFailure, grpcUnavailableErr),
			expected: message.EngineAgentConnectionTransportError,
			metadata: map[string]string{"grpcCode": codes.Unavailable.String()},
		},
		{
			name:     "ACK timeout is an ACK timeout",
			cause:    fmt.Errorf("%w: missed ACKs", errTetherAckTimeout),
			expected: message.EngineAgentConnectionAckTimeout,
		},
		{
			name:     "unknown failure is a transport error",
			cause:    assert.AnError,
			expected: message.EngineAgentConnectionTransportError,
		},
		{
			name:     "abort without cause is a transport error",
			expected: message.EngineAgentConnectionTransportError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := classifyAbortCause(tt.cause)

			assert.ErrorIs(t, err, message.New(tt.expected))
			if tt.metadata == nil {
				assert.Empty(t, message.IsMessage(err).Metadata())
			} else {
				assert.Equal(t, tt.metadata, message.IsMessage(err).Metadata())
			}
			if tt.cause != nil {
				assert.ErrorIs(t, err, tt.cause)
			}
		})
	}
}

func TestClassifyAbortCause_PreservesTransportDetailsAcrossGRPC(t *testing.T) {
	cause := fmt.Errorf(
		"%w while receiving ACK: %w",
		errTetherStreamFailure,
		status.Error(codes.Unavailable, "connection reset by peer"),
	)

	classified := classifyAbortCause(cause)
	rebuilt := message.FromGRPCStatus(message.AsGRPCStatus(classified))

	assert.ErrorIs(t, rebuilt, message.New(message.EngineAgentConnectionTransportError))
	assert.ErrorContains(t, rebuilt, codes.Unavailable.String())
	assert.ErrorContains(t, rebuilt, "connection reset by peer")
	rebuiltMessage := message.IsMessage(rebuilt)
	require.NotNil(t, rebuiltMessage)
	assert.Equal(t, codes.Unavailable.String(), rebuiltMessage.Metadata()["grpcCode"])
}

func TestAgentContext_PanicsWhenLifetimeContextIsNil(t *testing.T) {
	agentConn := &AgentConn{}

	assert.PanicsWithValue(t, noAgentCtxPanic, func() {
		_ = agentConn.AgentContext()
	})
}

func TestAgentConnBindContext(t *testing.T) {
	t.Run("agent disconnect propagates cause", func(t *testing.T) {
		agentConn := &AgentConn{}
		agentConn.initLifetimeContext()

		boundCtx, _ := agentConn.BindContext(context.Background())
		agentConn.cancel(ErrAgentDisconnected)

		select {
		case <-boundCtx.Done():
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for bound context cancellation")
		}

		assert.ErrorIs(t, context.Cause(boundCtx), ErrAgentDisconnected)
	})

	t.Run("parent cancel wins", func(t *testing.T) {
		agentConn := &AgentConn{}
		agentConn.initLifetimeContext()

		parentCtx, cancelParent := context.WithCancelCause(context.Background())
		boundCtx, _ := agentConn.BindContext(parentCtx)

		parentCancelErr := errors.New("parent cancelled")
		cancelParent(parentCancelErr)

		select {
		case <-boundCtx.Done():
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for bound context cancellation")
		}

		assert.Equal(t, parentCancelErr, context.Cause(boundCtx))
	})
}

func TestNewConnection_FailsWhenToolsPathDeletedAfterPrepare(t *testing.T) {
	cmdRunner := &conductormocks.MockCommandRunner{}
	// Initial prepare succeeds
	cmdRunner.On("RunCommand", "test -e /tmp/tools").Return("", "", nil).Once()
	cmdRunner.On("RunCommand", "test -w /tmp/tools").Return("", "", nil).Once()
	_, err := conductor.ResolveToolsBaseDir("/tmp/tools", conductor.Linux, cmdRunner, locality.Target)
	require.NoError(t, err)

	// Later resolve (during GetAgentPath) sees the path missing
	cmdRunner.On("RunCommand", "test -e /tmp/tools").Return("", "", assert.AnError).Once()

	p := &agentConnectionCreator{
		toolsDir: "/tmp/tools",
		launch: func(_ conductor.TargetPlatform, agentPath string, _ bool, _ string) (int, error) {
			assert.Empty(t, agentPath)
			return 0, assert.AnError
		},
	}

	tp := conductor.TargetPlatform{
		PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Linux},
		Path:                  &conductor.LinuxPathUtils{},
		Actions:               &conductormocks.MockTargetActions{CmdRunner: cmdRunner},
	}

	ac, err := p.NewConnection("target_agent", locality.Target, tp, nil)
	assert.Error(t, err)
	assert.Nil(t, ac)
	cmdRunner.AssertExpectations(t)
}

func TestNewConnection_FailsWhenToolsPathBecomesUnwritableAfterPrepare(t *testing.T) {
	cmdRunner := &conductormocks.MockCommandRunner{}
	// Initial prepare succeeds
	cmdRunner.On("RunCommand", "test -e /tmp/tools").Return("", "", nil).Once()
	cmdRunner.On("RunCommand", "test -w /tmp/tools").Return("", "", nil).Once()
	_, err := conductor.ResolveToolsBaseDir("/tmp/tools", conductor.Linux, cmdRunner, locality.Target)
	require.NoError(t, err)

	// Later resolve (during GetAgentPath) finds it unwritable
	cmdRunner.On("RunCommand", "test -e /tmp/tools").Return("", "", nil).Once()
	cmdRunner.On("RunCommand", "test -w /tmp/tools").Return("", "", assert.AnError).Once()

	p := &agentConnectionCreator{
		toolsDir: "/tmp/tools",
		launch: func(_ conductor.TargetPlatform, agentPath string, _ bool, _ string) (int, error) {
			assert.Empty(t, agentPath)
			return 0, assert.AnError
		},
	}

	tp := conductor.TargetPlatform{
		PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Linux},
		Path:                  &conductor.LinuxPathUtils{},
		Actions:               &conductormocks.MockTargetActions{CmdRunner: cmdRunner},
	}

	ac, err := p.NewConnection("target_agent", locality.Target, tp, nil)
	assert.Error(t, err)
	assert.Nil(t, ac)
	cmdRunner.AssertExpectations(t)
}

type testAgentTether struct {
	start func() error
}

func (t *testAgentTether) Start() error {
	return t.start()
}

func (t *testAgentTether) OnTransportFailure(func(error)) {}

func (t *testAgentTether) Close() {}

type singlePulseTetherServer struct {
	tetherproto.UnimplementedTetherServer
}

func (s *singlePulseTetherServer) Hold(stream tetherproto.Tether_HoldServer) error {
	if _, err := stream.Recv(); err != nil {
		return err
	}
	return stream.Send(&tetherproto.PulseAck{})
}

type testTargetAgentServer struct {
	targetagentproto.UnimplementedTargetAgentServer
	shutdownCalls int32
	shutdownForce chan bool
}

func newTestTargetAgentServer() *testTargetAgentServer {
	return &testTargetAgentServer{shutdownForce: make(chan bool, 1)}
}

func (s *testTargetAgentServer) Shutdown(_ context.Context, req *targetagentproto.ShutdownRequest) (*emptypb.Empty, error) {
	atomic.AddInt32(&s.shutdownCalls, 1)
	s.shutdownForce <- req.GetForce()
	return &emptypb.Empty{}, nil
}

func newTestGRPCClientConn(t *testing.T, register func(*grpc.Server)) *grpc.ClientConn {
	t.Helper()

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	if register != nil {
		register(server)
	}

	go func() {
		_ = server.Serve(listener)
	}()

	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	cc, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cc.Close() })

	return cc
}
