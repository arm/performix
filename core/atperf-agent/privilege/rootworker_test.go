// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package privilege

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	agentmocks "github.com/Arm-Debug/apap-cli/apap-engine/agent/mocks"
	"github.com/Arm-Debug/apap-cli/atperf-agent/grpcserver/transport"
	"github.com/Arm-Debug/apap-cli/atperf-agent/ioutil"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/clients/go/healthproto"
	"github.com/Arm-Debug/apap-cli/clients/go/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

type fakeTransport struct {
	r      io.Reader
	w      io.Writer
	closed atomic.Bool
}

func (f *fakeTransport) Read(p []byte) (int, error) {
	if f.closed.Load() {
		return 0, io.EOF
	}
	return f.r.Read(p)
}
func (f *fakeTransport) Write(p []byte) (int, error) {
	if f.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	return f.w.Write(p)
}
func (f *fakeTransport) Close() error { f.closed.Store(true); return nil }

type stubHealth struct {
	seq []struct {
		resp *healthproto.HealthCheckResponse
		err  error
	}
	i             int
	defaultStatus *healthproto.HealthCheckResponse_ServingStatus
}

func (s *stubHealth) Check(ctx context.Context, _ *healthproto.HealthCheckRequest, _ ...grpc.CallOption) (*healthproto.HealthCheckResponse, error) {
	if s.i >= len(s.seq) {
		if s.defaultStatus == nil {
			return &healthproto.HealthCheckResponse{Status: healthproto.HealthCheckResponse_SERVING}, nil
		} else {
			return &healthproto.HealthCheckResponse{Status: *s.defaultStatus}, nil
		}
	}
	cur := s.seq[s.i]
	s.i++
	return cur.resp, cur.err
}

// newRootWorkerForTests wires tiny timeouts and test seams.
func newRootWorkerForTests(
	pm process.ProcessManager,
	af AcceptorFactory,
	agent targetagentproto.TargetAgentClient,
	hf healthproto.HealthClient,
	buildErr error,
) *rootWorkerProcessImpl {
	cfg := RootWorkerProcessConfig{
		TransportLoggingEnabled: false,
	}
	r, _ := newRootWorkerProcessImpl(pm, af, cfg)
	r.LaunchTiming = LaunchTiming{
		AcceptTimeout:   80 * time.Millisecond,
		HealthPollEvery: 5 * time.Millisecond,
	}
	FillDefaultLaunchTiming(&r.LaunchTiming)
	// Inject seams
	r.deps.buildClient = func(t transport.Transport) (targetagentproto.TargetAgentClient, *grpc.ClientConn, func(), error) {
		return agent, &grpc.ClientConn{}, func() {}, buildErr
	}
	r.deps.newHealth = func(_ grpc.ClientConnInterface) healthproto.HealthClient { return hf }
	r.deps.waitProcess = func(p *os.Process) (any, error) { return "exited", nil }
	return r
}

func TestLaunch_HappyPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)

	pm := &process.MockProcessManager{}
	af := &MockAcceptorFactory{}
	acc := &MockAcceptor{}
	xp := &fakeTransport{r: bytes.NewReader(nil), w: io.Discard}

	af.On("NewAcceptor").Return(acc, nil)
	acc.On("GetIPCAddress").Return("/tmp/sock")
	acc.On("Accept").Return(xp, nil)
	acc.On("Close").Return(nil)

	proc := &os.Process{Pid: 123}
	stderrStreamStarted := make(chan struct{})
	pm.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).Return(proc, nil)
	pm.On("StreamStderr", 123, mock.Anything).Run(func(args mock.Arguments) {
		close(stderrStreamStarted)
	}).Return(nil)
	pm.On("WriteToStdin", 123, []byte{}).Return(nil)

	// health: immediate SERVING
	hf := &stubHealth{seq: []struct {
		resp *healthproto.HealthCheckResponse
		err  error
	}{{resp: &healthproto.HealthCheckResponse{Status: healthproto.HealthCheckResponse_SERVING}}}}

	agent := mocks.NewTargetAgentClient(t)
	r := newRootWorkerForTests(pm, af, agent, hf, nil)

	client, err := r.Launch(ctx)
	require.NoError(t, err)
	require.NotNil(t, client)
	require.NotNil(t, r.cleanup)
	require.NotNil(t, r.proc)

	// StreamStderr launches a goroutine which might take some time to start
	// Wait for it to avoid failures on slow CI runners
	select {
	case <-stderrStreamStarted:
		// ok
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for StreamStderr to start")
	}

	// cleanup
	r.Close()
	af.AssertExpectations(t)
	acc.AssertExpectations(t)
	pm.AssertExpectations(t)
}

func TestLaunch_AcceptorFactoryError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	t.Cleanup(cancel)

	pm := &process.MockProcessManager{}
	af := &MockAcceptorFactory{}
	af.On("NewAcceptor").Return(nil, errors.New("boom"))

	agent := mocks.NewTargetAgentClient(t)
	hf := &stubHealth{}
	r := newRootWorkerForTests(pm, af, agent, hf, nil)

	_, err := r.Launch(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to accept connection: boom")
	pm.AssertExpectations(t)
	af.AssertExpectations(t)
}

func TestLaunch_AcceptReturnsError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	t.Cleanup(cancel)

	pm := &process.MockProcessManager{}
	af := &MockAcceptorFactory{}
	acc := &MockAcceptor{}

	af.On("NewAcceptor").Return(acc, nil)
	acc.On("GetIPCAddress").Return("/tmp/sock")
	acc.On("Accept").Return(nil, errors.New("accept fail"))
	acc.On("Close").Return(nil)

	proc := &os.Process{Pid: 123}
	pm.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).Return(proc, nil)
	pm.On("StreamStderr", 123, mock.Anything).Return(nil).Maybe()
	pm.On("WriteToStdin", 123, []byte{}).Return(nil)

	agent := mocks.NewTargetAgentClient(t)
	hf := &stubHealth{}
	r := newRootWorkerForTests(pm, af, agent, hf, nil)

	_, err := r.Launch(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accept transport connection")
	acc.AssertCalled(t, "Close")
	pm.AssertExpectations(t)
}

func TestLaunch_AcceptTimeout(t *testing.T) {
	// Context survives longer than our short AcceptTimeout but is canceled on exit
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	pm := &process.MockProcessManager{}
	af := &MockAcceptorFactory{}
	acc := &MockAcceptor{}

	af.On("NewAcceptor").Return(acc, nil)
	acc.On("GetIPCAddress").Return("/tmp/sock")
	// Accept() doesn't deliver - simulate by blocking until ctx is done on our fake
	acc.On("Accept").Run(func(mock.Arguments) { <-ctx.Done() }).Return(nil, context.Canceled)
	acc.On("Close").Return(nil)

	proc := &os.Process{Pid: 123}
	pm.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).Return(proc, nil)
	pm.On("StreamStderr", 123, mock.Anything).Return(nil).Maybe()
	pm.On("WriteToStdin", 123, []byte{}).Return(nil)

	agent := mocks.NewTargetAgentClient(t)
	hf := &stubHealth{}
	r := newRootWorkerForTests(pm, af, agent, hf, nil)
	// tighten AcceptTimeout further for determinism
	r.LaunchTiming.AcceptTimeout = 40 * time.Millisecond

	_, err := r.Launch(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to accept connection from root-worker")
	assert.ErrorIs(t, errors.Unwrap(err), context.DeadlineExceeded)
	acc.AssertCalled(t, "Close")
	pm.AssertExpectations(t)
}

func TestLaunch_ContextCanceledDuringAccept(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	pm := &process.MockProcessManager{}
	af := &MockAcceptorFactory{}
	acc := &MockAcceptor{}

	af.On("NewAcceptor").Return(acc, nil)
	acc.On("GetIPCAddress").Return("/tmp/sock")
	acc.On("Accept").Return(func() transport.Transport {
		// cancel before we "return"
		cancel()
		<-time.After(5 * time.Millisecond)
		return nil
	}(), context.Canceled)
	acc.On("Close").Return(nil)

	proc := &os.Process{Pid: 123}
	pm.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).Return(proc, nil)
	pm.On("StreamStderr", 123, mock.Anything).Return(nil).Maybe()
	pm.On("WriteToStdin", 123, []byte{}).Return(nil)

	agent := mocks.NewTargetAgentClient(t)
	hf := &stubHealth{}
	r := newRootWorkerForTests(pm, af, agent, hf, nil)

	_, err := r.Launch(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	acc.AssertCalled(t, "Close")
	pm.AssertExpectations(t)
}

func TestLaunch_buildClientError_CleanupInvoked(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	t.Cleanup(cancel)

	pm := &process.MockProcessManager{}
	af := &MockAcceptorFactory{}
	acc := &MockAcceptor{}
	xp := &fakeTransport{}

	af.On("NewAcceptor").Return(acc, nil)
	acc.On("GetIPCAddress").Return("/tmp/sock")
	acc.On("Accept").Return(xp, nil)
	acc.On("Close").Return(nil)

	proc := &os.Process{Pid: 456}
	pm.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).Return(proc, nil)
	pm.On("StreamStderr", 456, mock.Anything).Return(nil)
	pm.On("WriteToStdin", 456, []byte{}).Return(nil)

	agent := mocks.NewTargetAgentClient(t)
	hf := &stubHealth{}
	r := newRootWorkerForTests(pm, af, agent, hf, errors.New("dial fail"))

	_, err := r.Launch(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to build root-worker client")
	pm.AssertCalled(t, "WriteToStdin", 456, []byte{})
	acc.AssertCalled(t, "Close")
}

func TestLaunch_HealthFlapsThenServing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)

	pm := &process.MockProcessManager{}
	af := &MockAcceptorFactory{}
	acc := &MockAcceptor{}
	xp := &fakeTransport{}

	af.On("NewAcceptor").Return(acc, nil)
	acc.On("GetIPCAddress").Return("/tmp/sock")
	acc.On("Accept").Return(xp, nil)
	acc.On("Close").Return(nil)

	proc := &os.Process{Pid: 1}
	pm.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).Return(proc, nil)
	pm.On("StreamStderr", 1, mock.Anything).Return(nil)

	hf := &stubHealth{seq: []struct {
		resp *healthproto.HealthCheckResponse
		err  error
	}{
		{resp: &healthproto.HealthCheckResponse{Status: healthproto.HealthCheckResponse_UNKNOWN}},
		{err: errors.New("transient")},
		{resp: &healthproto.HealthCheckResponse{Status: healthproto.HealthCheckResponse_SERVING}},
	}}

	agent := mocks.NewTargetAgentClient(t)
	r := newRootWorkerForTests(pm, af, agent, hf, nil)
	r.LaunchTiming.HealthPollEvery = 5 * time.Millisecond
	r.LaunchTiming.HealthTimeout = 10 * time.Second

	client, err := r.Launch(ctx)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestLaunch_HealthNeverServing_TimesOut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)

	pm := &process.MockProcessManager{}
	af := &MockAcceptorFactory{}
	acc := &MockAcceptor{}
	xp := &fakeTransport{}

	af.On("NewAcceptor").Return(acc, nil)
	acc.On("GetIPCAddress").Return("/tmp/sock")
	acc.On("Accept").Return(xp, nil)
	acc.On("Close").Return(nil)

	proc := &os.Process{Pid: 7}
	pm.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).Return(proc, nil)
	pm.On("StreamStderr", 7, mock.Anything).Return(nil)
	pm.On("WriteToStdin", 7, []byte{}).Return(errors.New("stdin fail"))

	// health: forever NOT_SERVING
	defaultStatus := healthproto.HealthCheckResponse_NOT_SERVING
	hf := &stubHealth{defaultStatus: &defaultStatus}

	agent := mocks.NewTargetAgentClient(t)
	r := newRootWorkerForTests(pm, af, agent, hf, nil)
	r.LaunchTiming.AcceptTimeout = 60 * time.Millisecond
	r.LaunchTiming.HealthPollEvery = 5 * time.Millisecond
	r.LaunchTiming.HealthTimeout = 100 * time.Millisecond

	_, err := r.Launch(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout waiting for root-worker healthy")
	pm.AssertCalled(t, "WriteToStdin", 7, []byte{})
	acc.AssertCalled(t, "Close")
}

func TestLaunch_ContextCanceledDuringHealth(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pm := &process.MockProcessManager{}
	af := &MockAcceptorFactory{}
	acc := &MockAcceptor{}
	xp := &fakeTransport{}

	af.On("NewAcceptor").Return(acc, nil)
	acc.On("GetIPCAddress").Return("/tmp/sock")
	acc.On("Accept").Return(xp, nil)
	acc.On("Close").Return(nil)

	proc := &os.Process{Pid: 321}
	pm.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).Return(proc, nil)
	pm.On("StreamStderr", 321, mock.Anything).Return(nil)
	pm.On("WriteToStdin", 321, []byte{}).Return(nil)

	// Health never reaches SERVING; we’ll cancel the context to exit the loop.
	defaultStatus := healthproto.HealthCheckResponse_NOT_SERVING
	hf := &stubHealth{defaultStatus: &defaultStatus}

	agent := new(mocks.TargetAgentClient)
	r := newRootWorkerForTests(pm, af, agent, hf, nil)
	//r.LaunchTiming.HealthTimeout = 10 * time.Second

	// Kick off a cancel shortly after launch starts polling health.
	cancelSoon := make(chan struct{})
	go func() {
		close(cancelSoon)
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := r.Launch(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)

	// Cleanup should have been invoked.
	pm.AssertCalled(t, "WriteToStdin", 321, []byte{})
	acc.AssertCalled(t, "Close")
}

func TestClose_Idempotent(t *testing.T) {
	// Set up a launched instance, then Close twice; cleanup should run once.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)

	pm := &process.MockProcessManager{}
	af := &MockAcceptorFactory{}
	acc := &MockAcceptor{}
	xp := &fakeTransport{}

	af.On("NewAcceptor").Return(acc, nil)
	acc.On("GetIPCAddress").Return("/tmp/sock")
	acc.On("Accept").Return(xp, nil)
	acc.On("Close").Return(nil)

	proc := &os.Process{Pid: 99}
	pm.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).Return(proc, nil)
	pm.On("StreamStderr", 99, mock.Anything).Return(nil)
	pm.On("WriteToStdin", 99, []byte{}).Return(nil)

	hf := &stubHealth{seq: []struct {
		resp *healthproto.HealthCheckResponse
		err  error
	}{{resp: &healthproto.HealthCheckResponse{Status: healthproto.HealthCheckResponse_SERVING}}}}
	agent := mocks.NewTargetAgentClient(t)
	r := newRootWorkerForTests(pm, af, agent, hf, nil)

	_, err := r.Launch(ctx)
	require.NoError(t, err)

	r.Close()
	r.Close() // should be no-op
	pm.AssertNumberOfCalls(t, "WriteToStdin", 1)
	acc.AssertNumberOfCalls(t, "Close", 1)
}

func TestStreamLogs_ClientNotInitialized(t *testing.T) {
	var r rootWorkerProcessImpl
	err := r.StreamLogs(log.StandardLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

func TestStreamLogs_NilEntryThenClose_ReturnsNil(t *testing.T) {
	// We avoid asserting exact converted message text; we only verify control flow & no error.
	cw := ioutil.NewCutoffWriter()
	orig := log.StandardLogger().Out
	log.SetOutput(cw)
	t.Cleanup(func() { cw.Cutoff(); log.SetOutput(orig) })

	agent := mocks.NewTargetAgentClient(t)
	stream := &agentmocks.StubLogStream{
		Entries: []any{nil}, // nil entry triggers warning path
	}
	agent.On("StreamLogs", mock.Anything, mock.Anything).Return(stream, nil)

	r := &rootWorkerProcessImpl{client: agent}
	err := r.StreamLogs(log.StandardLogger())
	require.NoError(t, err)

	cw.Cutoff()
	got := cw.String()
	assert.Contains(t, got, "Received nil log entry") // warning path
}

func TestStreamLogs_RecvError_Propagates(t *testing.T) {
	agent := mocks.NewTargetAgentClient(t)
	stream := &agentmocks.StubLogStream{Err: errors.New("recv fail")}
	agent.On("StreamLogs", mock.Anything, mock.Anything).Return(stream, nil)

	r := &rootWorkerProcessImpl{client: agent}
	err := r.StreamLogs(log.StandardLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error receiving log entry")
}

func TestStreamLogs_HappyPath(t *testing.T) {
	// Capture log output.
	cw := ioutil.NewCutoffWriter()
	orig := log.StandardLogger().Out
	log.SetOutput(cw)
	t.Cleanup(func() { cw.Cutoff(); log.SetOutput(orig) })

	agent := mocks.NewTargetAgentClient(t)
	stream := &agentmocks.StubLogStream{
		Entries: []any{
			&targetagentproto.LogEntry{Message: "first"},
			&targetagentproto.LogEntry{Message: "second"},
		},
		// After entries are consumed, StubLogStream returns grpc.ErrClientConnClosing.
	}
	agent.On("StreamLogs", mock.Anything, mock.Anything).Return(stream, nil)

	r := &rootWorkerProcessImpl{client: agent}
	err := r.StreamLogs(log.StandardLogger())
	require.NoError(t, err)

	// We don’t assert exact formatting from conversion; just that our prefix made it through.
	out := cw.String()
	assert.Contains(t, out, "[RootWorker]")
}

func TestCheckPrivileges_And_LogVersion(t *testing.T) {
	agent := mocks.NewTargetAgentClient(t)
	agent.On("GetPrivilegeInfo", mock.Anything, mock.Anything).
		Return(&targetagentproto.GetPrivilegeInfoResponse{HasAdmin: true}, nil).Once()
	agent.On("GetVersion", mock.Anything, mock.Anything).
		Return(&targetagentproto.GetVersionResponse{Version: "1.2.3"}, nil).Once()

	r := &rootWorkerProcessImpl{client: agent}

	ok, err := r.CheckPrivileges(context.Background())
	require.NoError(t, err)
	assert.True(t, ok)

	cw := ioutil.NewCutoffWriter()
	orig := log.StandardLogger().Out
	log.SetOutput(cw)
	defer func() { cw.Cutoff(); log.SetOutput(orig) }()

	require.NoError(t, r.LogVersion(context.Background()))
	assert.Contains(t, cw.String(), "Root-worker version: 1.2.3")
}

func TestStartWatchdog_InvokesCallback(t *testing.T) {
	called := make(chan struct{}, 1)

	r := &rootWorkerProcessImpl{}
	// Seam: waitProcess immediately returns; callback should run.
	r.deps.waitProcess = func(p *os.Process) (any, error) { return "ok", nil }
	r.proc = &os.Process{Pid: 42}

	r.StartWatchdog(func() { called <- struct{}{} })

	select {
	case <-called:
		// ok
	case <-time.After(200 * time.Millisecond):
		t.Fatal("watchdog callback not invoked")
	}
}

func TestRedirectStderrToLogger_ToleratesError(t *testing.T) {
	pm := &process.MockProcessManager{}
	pm.On("StreamStderr", 101, mock.Anything).Return(errors.New("stream fail"))

	// Should not panic.
	redirectStderrToLogger(&os.Process{Pid: 101}, pm)
	// Give the goroutine a tick to run
	time.Sleep(10 * time.Millisecond)
	pm.AssertExpectations(t)
}

func TestRedirectStderrToLogger_HappyPath(t *testing.T) {
	// Capture log output to verify stderr lines are logged.
	cw := ioutil.NewCutoffWriter()
	orig := log.StandardLogger().Out
	log.SetOutput(cw)
	t.Cleanup(func() { cw.Cutoff(); log.SetOutput(orig) })

	pm := &process.MockProcessManager{}
	// When redirectStderrToLogger calls StreamStderr with our adapter, simulate the worker emitting a line.
	pm.On("StreamStderr", 777, mock.Anything).Run(func(args mock.Arguments) {
		sender := args.Get(1).(process.StreamChunkSender)
		_ = sender.Send(&process.StreamChunk{Data: []byte("hello stderr")})
		// Return cleanly.
	}).Return(nil)

	redirectStderrToLogger(&os.Process{Pid: 777}, pm)

	// Give the goroutine a moment to run.
	time.Sleep(20 * time.Millisecond)

	output := cw.String()
	assert.Contains(t, output, "[RootWorker stderr] hello stderr")
	pm.AssertExpectations(t)
}
