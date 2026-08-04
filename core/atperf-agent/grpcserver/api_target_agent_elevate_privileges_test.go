// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/atperf-agent/privilege"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// makeClient creates a typed (but inert) gRPC client we never actually use.
func makeClient() targetagentproto.TargetAgentClient {
	// NewTargetAgentClient only needs something that satisfies grpc.ClientConnInterface;
	// a zero *grpc.ClientConn is fine since we never call it.
	return targetagentproto.NewTargetAgentClient(&grpc.ClientConn{})
}

// factoryReturning returns a RootWorkerProcessFactory that yields (rw, err).
func factoryReturning(rw privilege.RootWorkerProcess, err error) privilege.RootWorkerProcessFactory {
	return func(_ process.ProcessManager, _ privilege.AcceptorFactory, _ privilege.RootWorkerProcessConfig) (privilege.RootWorkerProcess, error) {
		return rw, err
	}
}

// noPasswdSudoRequest builds an ElevatePrivilegesRequest for the no password sudo mechanism.
func noPasswdSudoRequest() *targetagentproto.ElevatePrivilegesRequest {
	return &targetagentproto.ElevatePrivilegesRequest{
		Proof: &targetagentproto.PrivilegeProof{
			Mech: &targetagentproto.PrivilegeProof_NoPasswdSudo{
				NoPasswdSudo: true,
			},
		},
	}
}

// notElevatedChecker returns a MockChecker that reports the process is not privileged.
// Use this in tests that need ElevatePrivileges to proceed past the privilege check.
func notElevatedChecker() privilege.Checker {
	m := &privilege.MockChecker{}
	m.On("IsPrivileged").Return(false, nil)
	return m
}

func TestElevator_AndroidSuNotImplemented(t *testing.T) {
	factoryCalled := false

	elevator := &Elevator{}
	err := elevator.ElevatePrivileges(context.Background(), ElevatorConfig{
		ProofMechanism: privilege.AndroidSu,
		RootWorkerFactory: func(_ process.ProcessManager, _ privilege.AcceptorFactory, _ privilege.RootWorkerProcessConfig) (privilege.RootWorkerProcess, error) {
			factoryCalled = true
			return nil, nil
		},
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, message.New(message.AgentElevatePrivilegesMechanismAndroidSuNotImplemented))
	assert.False(t, factoryCalled)
}

func TestElevatePrivileges_AlreadyRunning_NoOp(t *testing.T) {
	ctx := context.Background()

	// If rootWorker is already set, ElevatePrivileges should short-circuit and return a token.
	existing := &privilege.MockRootWorkerProcess{}

	tokenStorage, err := privilege.NewTokenStorage(privilege.WithDefaultTokenStorageConfig())
	require.NoError(t, err)

	s := &AgentServerAPI{
		RootWorkerFactory: factoryReturning(existing, nil),
		Elevator:          Elevator{rootWorker: existing},
		TokenStorage:      tokenStorage,
		Checker:           notElevatedChecker(),
	}

	resp, err := s.ElevatePrivileges(ctx, noPasswdSudoRequest())
	assert.NoError(t, err)
	assert.NotNil(t, resp.Token)

	// Ensure we didn't touch the mock at all.
	existing.AssertExpectations(t) // no expectations were set; ensures no unexpected calls happened
}

func TestElevatePrivileges_FactoryError(t *testing.T) {
	ctx := context.Background()

	tokenStorage, err := privilege.NewTokenStorage(privilege.WithDefaultTokenStorageConfig())
	require.NoError(t, err)

	factoryBoom := errors.New("factory boom")
	s := &AgentServerAPI{
		RootWorkerFactory: factoryReturning(nil, factoryBoom),
		TokenStorage:      tokenStorage,
		Checker:           notElevatedChecker(),
	}

	resp, err := s.ElevatePrivileges(ctx, noPasswdSudoRequest())
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.True(t, rootWorkerIsNil(s))

	expectedErr := message.New(message.AgentElevatePrivilegesMechanismPasswordlessSudo)
	assert.True(t, errors.Is(err, expectedErr))
}

func TestElevatePrivileges_TokenStorageNilError(t *testing.T) {
	ctx := context.Background()

	s := &AgentServerAPI{
		RootWorkerFactory: factoryReturning(nil, nil),
		TokenStorage:      nil, // should be non-nil
	}

	_, err := s.ElevatePrivileges(ctx, noPasswdSudoRequest())
	require.Error(t, err)

	expectedErr := message.New(message.AgentApiInternalError)
	assert.True(t, rootWorkerIsNil(s))
	assert.True(t, errors.Is(err, expectedErr))
}

func TestElevatePrivileges_ProofErrors(t *testing.T) {
	ctx := context.Background()

	tokenStorage, err := privilege.NewTokenStorage(privilege.WithDefaultTokenStorageConfig())
	require.NoError(t, err)

	s := &AgentServerAPI{
		RootWorkerFactory: factoryReturning(nil, nil),
		TokenStorage:      tokenStorage,
		Checker:           notElevatedChecker(),
	}

	t.Run("unknown mechanism", func(t *testing.T) {
		req := &targetagentproto.ElevatePrivilegesRequest{
			Proof: nil,
		}

		resp, err := s.ElevatePrivileges(ctx, req)
		require.Error(t, err)
		require.Nil(t, resp)
		require.True(t, rootWorkerIsNil(s))

		expectedErr := message.New(message.AgentElevatePrivilegesProofMechanismUnknown).
			WithCause(fmt.Errorf("missing request or proof mechanism"))
		assert.Equal(t, expectedErr, err)
	})

	t.Run("unsupported mechanism: NoPasswdUserns", func(t *testing.T) {
		req := &targetagentproto.ElevatePrivilegesRequest{
			Proof: &targetagentproto.PrivilegeProof{
				Mech: &targetagentproto.PrivilegeProof_NoPasswdUserns{
					NoPasswdUserns: true,
				},
			},
		}

		resp, err := s.ElevatePrivileges(ctx, req)
		require.Error(t, err)
		require.Nil(t, resp)
		require.True(t, rootWorkerIsNil(s))

		expectedErr := message.New(message.AgentElevatePrivilegesProofMechanismNotSupported).
			WithMetadata(map[string]string{"mech": "NoPasswdUserns"})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("unsupported mechanism: SudoPassword", func(t *testing.T) {
		req := &targetagentproto.ElevatePrivilegesRequest{
			Proof: &targetagentproto.PrivilegeProof{
				Mech: &targetagentproto.PrivilegeProof_SudoPassword{
					SudoPassword: &targetagentproto.SudoPassword{
						Value: "test-password",
					},
				},
			},
		}

		resp, err := s.ElevatePrivileges(ctx, req)
		require.Error(t, err)
		require.Nil(t, resp)
		require.True(t, rootWorkerIsNil(s))

		expectedErr := message.New(message.AgentElevatePrivilegesProofMechanismNotSupported).
			WithMetadata(map[string]string{"mech": "SudoPassword"})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("unsupported mechanism: SetuidHelper", func(t *testing.T) {
		req := &targetagentproto.ElevatePrivilegesRequest{
			Proof: &targetagentproto.PrivilegeProof{
				Mech: &targetagentproto.PrivilegeProof_SetuidHelper{
					SetuidHelper: true,
				},
			},
		}

		resp, err := s.ElevatePrivileges(ctx, req)
		require.Error(t, err)
		require.Nil(t, resp)
		require.True(t, rootWorkerIsNil(s))

		expectedErr := message.New(message.AgentElevatePrivilegesProofMechanismNotSupported).
			WithMetadata(map[string]string{"mech": "SetuidHelper"})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}

func TestElevatePrivileges_LaunchError(t *testing.T) {
	ctx := context.Background()

	rw := &privilege.MockRootWorkerProcess{}
	launchBoom := errors.New("launch boom")
	rw.On("Launch", mock.Anything).Return(makeClient(), launchBoom)

	tokenStorage, err := privilege.NewTokenStorage(privilege.WithDefaultTokenStorageConfig())
	require.NoError(t, err)

	s := &AgentServerAPI{
		RootWorkerFactory: factoryReturning(rw, nil),
		TokenStorage:      tokenStorage,
		Checker:           notElevatedChecker(),
	}

	resp, err := s.ElevatePrivileges(ctx, noPasswdSudoRequest())
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.True(t, rootWorkerIsNil(s))

	expectedErr := message.New(message.AgentElevatePrivilegesMechanismPasswordlessSudo)
	assert.True(t, errors.Is(err, expectedErr))
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))

	rw.AssertExpectations(t)
}

func TestElevatePrivileges_LogVersionError_CleansUp(t *testing.T) {
	ctx := context.Background()

	rw := &privilege.MockRootWorkerProcess{}
	rw.On("Launch", mock.Anything).Return(makeClient(), nil)
	versionBoom := errors.New("version boom")
	rw.On("LogVersion", mock.Anything).Return(versionBoom)
	rw.On("Close").Return()

	tokenStorage, err := privilege.NewTokenStorage(privilege.WithDefaultTokenStorageConfig())
	require.NoError(t, err)

	s := &AgentServerAPI{
		RootWorkerFactory: factoryReturning(rw, nil),
		TokenStorage:      tokenStorage,
		Checker:           notElevatedChecker(),
	}

	resp, err := s.ElevatePrivileges(ctx, noPasswdSudoRequest())
	require.Error(t, err)
	assert.Nil(t, resp)
	// Should not keep the root worker on failure.
	assert.True(t, rootWorkerIsNil(s))

	expectedErr := message.New(message.AgentElevatePrivilegesMechanismPasswordlessSudo)
	assert.True(t, errors.Is(err, expectedErr))
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))

	rw.AssertExpectations(t)
}

func TestElevatePrivileges_CheckPrivilegesError_CleansUp(t *testing.T) {
	ctx := context.Background()

	rw := &privilege.MockRootWorkerProcess{}
	rw.On("Launch", mock.Anything).Return(makeClient(), nil)
	rw.On("LogVersion", mock.Anything).Return(nil)
	privCheckBoom := errors.New("priv check boom")
	rw.On("CheckPrivileges", mock.Anything).Return(false, privCheckBoom)
	rw.On("Close").Return()

	tokenStorage, err := privilege.NewTokenStorage(privilege.WithDefaultTokenStorageConfig())
	require.NoError(t, err)

	s := &AgentServerAPI{
		RootWorkerFactory: factoryReturning(rw, nil),
		TokenStorage:      tokenStorage,
		Checker:           notElevatedChecker(),
	}

	resp, err := s.ElevatePrivileges(ctx, noPasswdSudoRequest())
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.True(t, rootWorkerIsNil(s))

	expectedErr := message.New(message.AgentElevatePrivilegesMechanismPasswordlessSudo)
	assert.True(t, errors.Is(err, expectedErr))
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))

	rw.AssertExpectations(t)
}

func TestElevatePrivileges_NotPrivileged_CleansUp(t *testing.T) {
	ctx := context.Background()

	rw := &privilege.MockRootWorkerProcess{}
	rw.On("Launch", mock.Anything).Return(makeClient(), nil)
	rw.On("LogVersion", mock.Anything).Return(nil)
	rw.On("CheckPrivileges", mock.Anything).Return(false, nil)
	rw.On("Close").Return()

	tokenStorage, err := privilege.NewTokenStorage(privilege.WithDefaultTokenStorageConfig())
	require.NoError(t, err)

	s := &AgentServerAPI{
		RootWorkerFactory: factoryReturning(rw, nil),
		TokenStorage:      tokenStorage,
		Checker:           notElevatedChecker(),
	}

	resp, err := s.ElevatePrivileges(ctx, noPasswdSudoRequest())
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.True(t, rootWorkerIsNil(s))

	expectedErr := message.New(message.AgentElevatePrivilegesMechanismPasswordlessSudo)
	assert.True(t, errors.Is(err, expectedErr))
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))

	rw.AssertExpectations(t)
}

func TestElevatePrivileges_InternalError(t *testing.T) {
	ctx := context.Background()

	tokenStorage := &privilege.MockTokenStorage{}
	generateBoom := errors.New("generate boom")
	tokenStorage.On("Generate").Return("", generateBoom)

	s := &AgentServerAPI{
		RootWorkerFactory: factoryReturning(nil, nil),
		TokenStorage:      tokenStorage,
	}

	resp, err := s.ElevatePrivileges(ctx, noPasswdSudoRequest())
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.True(t, rootWorkerIsNil(s))

	expectedErr := message.New(message.AgentApiInternalError)
	assert.True(t, errors.Is(err, expectedErr))

	tokenStorage.AssertExpectations(t)
}

func TestElevatePrivileges_Success_SetsRootWorker_AndStartsWatchdogAndLogs(t *testing.T) {
	ctx := context.Background()

	rw := &privilege.MockRootWorkerProcess{}

	// We’ll capture the watchdog function to exercise CleanupRootWorker quickly.
	var watchdog func()

	rw.On("Launch", mock.Anything).Return(makeClient(), nil)
	rw.On("StreamLogs", mock.Anything).Return(nil).Maybe()
	rw.On("StartWatchdog", mock.Anything).
		Run(func(args mock.Arguments) {
			// Save the callback; we'll invoke it after success to simulate the worker dying.
			watchdog = args.Get(0).(func())
		}).Return()
	rw.On("LogVersion", mock.Anything).Return(nil)
	rw.On("CheckPrivileges", mock.Anything).Return(true, nil)

	// When CleanupRootWorker is called (via watchdog), Close() should be invoked.
	rw.On("Close").Return()

	tokenStorage, err := privilege.NewTokenStorage(privilege.WithDefaultTokenStorageConfig())
	require.NoError(t, err)

	s := &AgentServerAPI{
		RootWorkerFactory: factoryReturning(rw, nil),
		TokenStorage:      tokenStorage,
		Checker:           notElevatedChecker(),
	}

	resp, err := s.ElevatePrivileges(ctx, noPasswdSudoRequest())
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	// On success, the server should retain the worker.
	assert.Same(t, rw, s.rootWorker)

	// Fire the watchdog to ensure CleanupRootWorker works and clears the field.
	if watchdog != nil {
		watchdog()
		// Cleanup happens in the watchdog; give the goroutine (if any) a tick.
		time.Sleep(5 * time.Millisecond)
		assert.True(t, rootWorkerIsNil(s))
	}

	rw.AssertExpectations(t)
}

func TestCleanupRootWorker_Idempotent(t *testing.T) {
	s := &AgentServerAPI{}
	// No panic, no effect when nil.
	s.CleanupRootWorker()

	// When non-nil, Close() is called and pointer cleared.
	rw := &privilege.MockRootWorkerProcess{}
	rw.On("Close").Return()

	s.rootWorker = rw
	s.CleanupRootWorker()

	assert.True(t, rootWorkerIsNil(s))
	rw.AssertExpectations(t)
}

func rootWorkerIsNil(s *AgentServerAPI) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rootWorker == nil
}

func rootWorkerIs(s *AgentServerAPI, rw privilege.RootWorkerProcess) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rootWorker == rw
}

func TestElevatePrivileges_StreamLogsFailure_CleansCurrentAfterReturn(t *testing.T) {
	ctx := context.Background()

	rw := &privilege.MockRootWorkerProcess{}
	rw.On("Launch", mock.Anything).Return(makeClient(), nil)
	rw.On("StartWatchdog", mock.Anything).Return()
	rw.On("LogVersion", mock.Anything).Return(nil)
	rw.On("CheckPrivileges", mock.Anything).Return(true, nil)

	// Gate the StreamLogs goroutine so it cannot call cleanup until we allow it.
	releaseStream := make(chan struct{})
	rw.On("StreamLogs", mock.Anything).Return(assert.AnError).Run(func(args mock.Arguments) {
		<-releaseStream // block until test allows the failure to proceed
	})

	// Expect Close() exactly once as part of cleanup of this instance.
	rw.On("Close").Once().Return()

	tokenStorage, err := privilege.NewTokenStorage(privilege.WithDefaultTokenStorageConfig())
	require.NoError(t, err)

	s := &AgentServerAPI{
		RootWorkerFactory: factoryReturning(rw, nil),
		TokenStorage:      tokenStorage,
		Checker:           notElevatedChecker(),
	}

	// Call ElevatePrivileges; it should set rootWorker and return even though StreamLogs is blocked.
	resp, err := s.ElevatePrivileges(ctx, noPasswdSudoRequest())
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.False(t, rootWorkerIsNil(s), "rootWorker should be set before stream error cleanup")

	// Now let StreamLogs fail and trigger cleanup.
	close(releaseStream)

	// Wait for cleanup to run.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) && !rootWorkerIsNil(s) {
		time.Sleep(5 * time.Millisecond)
	}

	assert.True(t, rootWorkerIsNil(s), "rootWorker should be cleared after StreamLogs failure cleanup")
	rw.AssertExpectations(t)
}

func TestElevatePrivileges_ConcurrentCalls_OnlyOneLaunch(t *testing.T) {
	ctx := context.Background()

	rw := &privilege.MockRootWorkerProcess{}
	rw.On("Launch", mock.Anything).Return(makeClient(), nil).Once()
	rw.On("StreamLogs", mock.Anything).Return(nil).Maybe()
	rw.On("StartWatchdog", mock.Anything).Return().Once()
	rw.On("LogVersion", mock.Anything).Return(nil).Once()
	rw.On("CheckPrivileges", mock.Anything).Return(true, nil).Once()

	tokenStorage, err := privilege.NewTokenStorage(privilege.WithDefaultTokenStorageConfig())
	require.NoError(t, err)

	var factoryCalls int32
	s := &AgentServerAPI{
		RootWorkerFactory: func(_ process.ProcessManager, _ privilege.AcceptorFactory, _ privilege.RootWorkerProcessConfig) (privilege.RootWorkerProcess, error) {
			atomic.AddInt32(&factoryCalls, 1)
			return rw, nil
		},
		TokenStorage: tokenStorage,
		Checker:      notElevatedChecker(),
	}

	const N = 16
	var wg sync.WaitGroup
	wg.Add(N)

	errCh := make(chan error, N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			_, err := s.ElevatePrivileges(ctx, noPasswdSudoRequest())
			errCh <- err
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		assert.NoError(t, err)
	}

	assert.Equal(t, int32(1), atomic.LoadInt32(&factoryCalls), "factory should be invoked exactly once")
	assert.False(t, rootWorkerIsNil(s), "rootWorker should be set after first success")

	rw.AssertExpectations(t)
}

// Stale StreamLogs callback must NOT close a newer rootWorker.
//
// Sequence:
//  1. First ElevatePrivileges succeeds (rw1). StreamLogs is blocked.
//  2. Watchdog for rw1 fires -> cleans up rw1 (pointer becomes nil).
//  3. Second ElevatePrivileges succeeds (rw2).
//  4. Now rw1's StreamLogs fails (late) and calls cleanup... this MUST NOT close rw2.
func TestElevatePrivileges_StaleStreamDoesNotKillNewWorker(t *testing.T) {
	ctx := context.Background()

	// --- First worker (rw1)
	rw1 := &privilege.MockRootWorkerProcess{}
	rw1.On("Launch", mock.Anything).Return(makeClient(), nil)

	var watchdog1 func()
	rw1.On("StartWatchdog", mock.Anything).Run(func(args mock.Arguments) {
		watchdog1 = args.Get(0).(func())
	}).Return()

	rw1.On("LogVersion", mock.Anything).Return(nil)
	rw1.On("CheckPrivileges", mock.Anything).Return(true, nil)

	// Gate rw1.StreamLogs so we can fire the error *after* rw2 is installed.
	stream1Go := make(chan struct{}) // signals that StreamLogs goroutine has started
	releaseStream1 := make(chan struct{})
	rw1.On("StreamLogs", mock.Anything).Return(assert.AnError).Run(func(args mock.Arguments) {
		close(stream1Go) // goroutine started
		<-releaseStream1 // wait until we allow the failure
	})

	// rw1 should be closed when its watchdog fires (once when the watchdog fires and once when StreamLogs returns).
	rw1.On("Close").Twice().Return()

	// --- Second worker (rw2)
	rw2 := &privilege.MockRootWorkerProcess{}
	rw2.On("Launch", mock.Anything).Return(makeClient(), nil)
	rw2.On("StartWatchdog", mock.Anything).Return()
	rw2.On("LogVersion", mock.Anything).Return(nil)
	rw2.On("CheckPrivileges", mock.Anything).Return(true, nil)

	// We don't care about rw2's StreamLogs in this test; let it run quickly.
	rw2.On("StreamLogs", mock.Anything).Return(nil)

	tokenStorage, err := privilege.NewTokenStorage(privilege.WithDefaultTokenStorageConfig())
	require.NoError(t, err)

	// Factory: first call returns rw1, second returns rw2.
	var once sync.Once
	s := &AgentServerAPI{
		RootWorkerFactory: func(_ process.ProcessManager, _ privilege.AcceptorFactory, _ privilege.RootWorkerProcessConfig) (privilege.RootWorkerProcess, error) {
			var out privilege.RootWorkerProcess
			once.Do(func() { out = rw1 })
			if out == nil {
				out = rw2
			}
			return out, nil
		},
		TokenStorage: tokenStorage,
		Checker:      notElevatedChecker(),
	}

	// 1) Start rw1; StreamLogs goroutine starts & blocks.
	_, err = s.ElevatePrivileges(ctx, noPasswdSudoRequest())
	assert.NoError(t, err)
	<-stream1Go

	// 2) Fire rw1 watchdog -> cleans up rw1 and unsets pointer.
	watchdog1()
	// Wait for pointer to clear.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) && !rootWorkerIsNil(s) {
		time.Sleep(5 * time.Millisecond)
	}
	assert.True(t, rootWorkerIsNil(s), "after rw1 watchdog, rootWorker should be nil")

	// 3) Start rw2.
	_, err = s.ElevatePrivileges(ctx, noPasswdSudoRequest())
	assert.NoError(t, err)
	assert.True(t, rootWorkerIs(s, rw2), "rw2 should be the current rootWorker")

	// 4) Now let rw1.StreamLogs fail *late*; this must not close rw2.
	close(releaseStream1)

	// Give cleanup a moment to run.
	time.Sleep(100 * time.Millisecond)

	// EXPECTATION: rw2.Close should NOT have been called by rw1's stale cleanup.
	rw2.AssertNotCalled(t, "Close")
	// And rw2 should remain current.
	assert.True(t, rootWorkerIs(s, rw2))

	rw1.AssertExpectations(t)
	rw2.AssertExpectations(t)
}

func TestElevatePrivileges_AlreadyElevated(t *testing.T) {
	ctx := context.Background()

	tokenStorage, err := privilege.NewTokenStorage(privilege.WithDefaultTokenStorageConfig())
	require.NoError(t, err)

	checker := &privilege.MockChecker{}
	checker.On("IsPrivileged").Return(true, nil)

	s := &AgentServerAPI{
		TokenStorage: tokenStorage,
		Checker:      checker,
	}

	resp, err := s.ElevatePrivileges(ctx, noPasswdSudoRequest())
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.Token)

	// No root worker should have been spawneds
	assert.True(t, rootWorkerIsNil(s))

	checker.AssertExpectations(t)
}

func TestElevatePrivileges_CheckerError(t *testing.T) {
	ctx := context.Background()

	tokenStorage, err := privilege.NewTokenStorage(privilege.WithDefaultTokenStorageConfig())
	require.NoError(t, err)

	checkerBoom := errors.New("checker boom")
	checker := &privilege.MockChecker{}
	checker.On("IsPrivileged").Return(false, checkerBoom)

	s := &AgentServerAPI{
		TokenStorage: tokenStorage,
		Checker:      checker,
	}

	resp, err := s.ElevatePrivileges(ctx, noPasswdSudoRequest())
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.True(t, rootWorkerIsNil(s))

	expectedErr := message.New(message.AgentApiInternalError)
	assert.True(t, errors.Is(err, expectedErr))

	checker.AssertExpectations(t)
}
