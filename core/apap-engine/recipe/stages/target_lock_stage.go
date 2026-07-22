// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func NewTargetLockStage(agentSupplier recipe.AgentConnSupplier, lockDuration time.Duration) *TargetLockStage {
	return &TargetLockStage{
		agentSupplier: agentSupplier,
		lockDuration:  lockDuration,
	}
}

type TargetLockStage struct {
	agentSupplier recipe.AgentConnSupplier
	lockDuration  time.Duration
	release       func()
}

// Execute tries to establish a HoldLock stream with the target agent which represents exclusive lock ownership.
func (t *TargetLockStage) Execute(stageCtx *recipe.StageContext) (func(), error) {

	lockGranted := atomic.Bool{}
	ctx, cancel := context.WithCancel(context.Background())
	stopCancelErr := make(chan error, 1)
	cmdStateChanel := stageCtx.CommandStateChannel
	if cmdStateChanel == nil {
		cmdStateChanel = cmdsync.NewDeadCommandStateChannel(stageCtx.Context)
	}
	mapError := func(baseErr chan error, targetErr error) error {
		select {
		case baseErr := <-baseErr:
			return baseErr
		default:
		}
		return targetErr
	}

	go func() {
		timeoutChan := make(<-chan time.Time)
		if t.lockDuration > 0 {
			timer := time.NewTimer(t.lockDuration)
			defer timer.Stop()
			timeoutChan = timer.C
		}

		// Observe Stop & Cancel. These only take effect before the lock is granted, after which the caller is responsible for cancelling via the returned cancel function.
		select {
		case <-cmdStateChanel.StopChan:
			if !lockGranted.Load() {
				stopCancelErr <- message.New(message.EngineCommonUserCancellationError)
				cancel()
			}
		case <-cmdStateChanel.CancelChan:
			if !lockGranted.Load() {
				stopCancelErr <- message.New(message.EngineCommonUserStoppedError)
				cancel()
			}
		case <-timeoutChan:
			if !lockGranted.Load() {
				stopCancelErr <- message.New(message.EngineRecipeTargetLockTimeout).WithMetadata(map[string]string{
					"timeout": t.lockDuration.String(),
				})
				cancel()
			}
		case <-ctx.Done(): // Timeout triggered, no action needed beyond completing this goroutine exits
		}
	}()

	// Release can be called once before artifact transfer starts and again by the
	// runtime cleanup path, so guard the shared lock cancellation.
	var releaseOnce sync.Once
	cancelLock := func() { releaseOnce.Do(cancel) }
	t.release = cancelLock
	stream, err := t.agentSupplier().Client.HoldLock(ctx, &emptypb.Empty{})
	if err != nil {
		return t.Release, mapError(stopCancelErr, err)
	}
	_, err = stream.Recv()
	if err == nil {
		lockGranted.Store(true)
	}
	// At this point we hold the target lock, or the connection returned an error. It is the callers responsibility to cancel via the returned cancel function.
	if err == io.EOF {
		return t.Release, message.Wrap(message.EngineAgentConnectionTransportError, err)
	}
	return t.Release, mapError(stopCancelErr, err)
}

// Release releases the target lock held by this stage. It is safe to call more
// than once and is a no-op before Execute has established a lock context.
func (t *TargetLockStage) Release() {
	if t.release != nil {
		t.release()
	}
}

func (t *TargetLockStage) Name() string {
	return "Acquiring target lock"
}

func (t *TargetLockStage) ErrorType() run.RunResult {
	return run.RecipeFailureTargetLock
}

func (t *TargetLockStage) AlwaysExecute() bool {
	return false
}
