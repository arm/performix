// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"bytes"
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

func TestContextWithInterruptStopCancelsContext(t *testing.T) {
	ctx, stop := ContextWithInterrupt(context.Background())
	t.Cleanup(stop)

	require.NoError(t, ctx.Err())

	stop()

	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestRegisterCallbackInitializesManager(t *testing.T) {
	cb := func(_ apapproto.ApapClient, cmd RecipeCommand) {
	}
	RegisterCallback(cb, nil)
	require.NotNil(t, cmdManager)
	require.Equal(t, len(cmdManager.receivers), 2)

}

func TestRecipeCommandManagerInvokesCallbacks(t *testing.T) {
	receiver := newTestReceiver()
	manager := newRecipeCommandManager(receiver)
	defer func() {
		close(manager.commandCh)
		receiver.close()
	}()

	received := make(chan RecipeCommand, 2)
	cb := func(_ apapproto.ApapClient, cmd RecipeCommand) {
		received <- cmd
	}

	manager.start()

	manager.registerCallback(cb, nil)
	manager.registerCallback(cb, nil)

	receiver.send(CANCEL)

	require.Equal(t, CANCEL, receiveCommand(t, received))
	require.Equal(t, CANCEL, receiveCommand(t, received))
}

func TestRecipeCommandManagerInvokesBufferedCommands(t *testing.T) {
	receiver := newTestReceiver()
	manager := newRecipeCommandManager(receiver)
	defer func() {
		close(manager.commandCh)
		receiver.close()
	}()

	received := make(chan RecipeCommand, 3)
	cb := func(_ apapproto.ApapClient, cmd RecipeCommand) {
		received <- cmd
	}

	manager.start()

	receiver.send(CANCEL)
	receiver.send(STOP)
	receiver.send(CANCEL)

	manager.registerCallback(cb, nil)

	require.Equal(t, CANCEL, receiveCommand(t, received))
	require.Equal(t, STOP, receiveCommand(t, received))
	require.Equal(t, CANCEL, receiveCommand(t, received))
}

func TestStdinReceiverMapsBytesToCommands(t *testing.T) {
	buffer := bytes.NewBuffer([]byte{'s', 'x', 'y', 'z', 'c', 'w', 's', 'q', 'q'})
	out := make(chan RecipeCommand, 2)
	receiver := newStdinReceiver(buffer, byteToCommand)
	receiver.start(out)

	require.Equal(t, STOP, receiveCommand(t, out))
	require.Equal(t, CANCEL, receiveCommand(t, out))
	require.Equal(t, STOP, receiveCommand(t, out))
	require.Equal(t, CANCEL, receiveCommand(t, out))
	require.Equal(t, CANCEL, receiveCommand(t, out))
}

func TestInterruptReceiverEmitsCommandsSequentially(t *testing.T) {
	notifier := &mockSignalNotifier{}

	var notified atomic.Bool
	var stop atomic.Bool

	notifier.On("Notify", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		notified.Store(true)
	}).Once()
	notifier.On("Reset", mock.Anything).Once()
	notifier.On("Stop", mock.Anything).Run(func(args mock.Arguments) {
		stop.Store(true)
	}).Once()

	out := make(chan RecipeCommand, 2)
	receiver := newInterruptReceiver(notifier, STOP, CANCEL)
	receiver.start(out)

	require.Eventually(t, func() bool { return notified.Load() }, time.Second, 10*time.Millisecond)

	notifier.emit(os.Interrupt)
	require.Equal(t, STOP, receiveCommand(t, out))

	notifier.emit(os.Interrupt)
	require.Equal(t, CANCEL, receiveCommand(t, out))

	require.Eventually(t, func() bool { return stop.Load() }, time.Second, 10*time.Millisecond)

	notifier.AssertExpectations(t)
}

func TestInterruptReceiverReraisesBufferedInterrupt(t *testing.T) {
	notifier := &mockSignalNotifier{}

	var notified atomic.Bool
	notifier.On("Notify", mock.Anything, mock.Anything).Once().Run(func(args mock.Arguments) {
		notified.Store(true)
	})
	notifier.On("Stop", mock.Anything).Once()
	notifier.On("Reset", mock.Anything).Run(func(args mock.Arguments) {
		// Simulate a buffered interrupt
		notifier.emit(os.Interrupt)
	}).Once()
	var signal atomic.Bool
	notifier.On("Signal", os.Interrupt).Run(func(args mock.Arguments) {
		signal.Store(true)
	}).Once().Return(nil)

	out := make(chan RecipeCommand, 2)
	receiver := newInterruptReceiver(notifier, STOP, CANCEL)
	receiver.start(out)

	require.Eventually(t, func() bool { return notified.Load() }, time.Second, 10*time.Millisecond)

	notifier.emit(os.Interrupt)
	require.Equal(t, STOP, receiveCommand(t, out))

	notifier.emit(os.Interrupt)
	require.Equal(t, CANCEL, receiveCommand(t, out))

	require.Eventually(t, func() bool { return signal.Load() }, time.Second, 10*time.Millisecond)

	notifier.AssertExpectations(t)
}

func receiveCommand(t *testing.T, ch <-chan RecipeCommand) RecipeCommand {
	t.Helper()
	select {
	case cmd := <-ch:
		return cmd
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for command")
		return 0
	}
}

type testReceiver struct {
	inputs chan RecipeCommand
}

func newTestReceiver() *testReceiver {
	return &testReceiver{inputs: make(chan RecipeCommand, 1)}
}

func (r *testReceiver) start(out chan<- RecipeCommand) {
	go func() {
		for cmd := range r.inputs {
			out <- cmd
		}
	}()
}

func (r *testReceiver) send(cmd RecipeCommand) {
	r.inputs <- cmd
}

func (r *testReceiver) close() {
	close(r.inputs)
}

type mockSignalNotifier struct {
	mock.Mock
	ch chan<- os.Signal
}

func (n *mockSignalNotifier) Notify(c chan<- os.Signal, sig ...os.Signal) {
	n.ch = c
	n.Called(c, sig)
}

func (n *mockSignalNotifier) Stop(c chan<- os.Signal) {
	n.Called(c)
}

func (n *mockSignalNotifier) Reset(sig ...os.Signal) {
	n.Called(sig)
}

func (n *mockSignalNotifier) Signal(sig os.Signal) error {
	args := n.Called(sig)
	return args.Error(0)
}

func (n *mockSignalNotifier) emit(sig os.Signal) {
	n.ch <- sig
}
