// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	tethermocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func newTestTether(deadline time.Duration) (*TetherServer, chan string) {
	reasonCh := make(chan string, 1)
	s := NewTetherServer(deadline, func(r string) { reasonCh <- r })
	return s, reasonCh
}

func TestHold_ClosedByController(t *testing.T) {
	s, reasons := newTestTether(30 * time.Millisecond)

	ms := tethermocks.NewTetherHoldServer(t)
	ms.On("Recv").Return(nil, io.EOF).Once()

	err := s.Hold(ms)
	assert.NoError(t, err)

	select {
	case r := <-reasons:
		assert.Equal(t, "tether closed by controller", r)
	case <-time.After(time.Second):
		t.Fatal("OnExpire not called")
	}

	ms.AssertExpectations(t)
}

func TestHold_ReceiveError(t *testing.T) {
	s, reasons := newTestTether(30 * time.Millisecond)

	ms := tethermocks.NewTetherHoldServer(t)
	ms.On("Recv").Return(nil, errors.New("boom")).Once()

	err := s.Hold(ms)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "tether receive error: boom")

	select {
	case r := <-reasons:
		assert.Equal(t, "tether receive error: boom", r)
	case <-time.After(time.Second):
		t.Fatal("OnExpire not called")
	}

	ms.AssertExpectations(t)
}

func TestHold_AckSendError(t *testing.T) {
	s, reasons := newTestTether(30 * time.Millisecond)

	ms := tethermocks.NewTetherHoldServer(t)
	ms.On("Recv").Return(nil, nil).Once()
	ms.On("Send", mock.Anything).Return(errors.New("ack send failed")).Once()

	err := s.Hold(ms)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "tether receive error: ack send failed")

	select {
	case r := <-reasons:
		assert.Equal(t, "tether receive error: ack send failed", r)
	case <-time.After(time.Second):
		t.Fatal("OnExpire not called")
	}

	ms.AssertExpectations(t)
}

func TestHold_NoPulses(t *testing.T) {
	s, reasons := newTestTether(30 * time.Millisecond)

	ms := tethermocks.NewTetherHoldServer(t)
	block := make(chan struct{})

	ms.On("Recv").Run(func(mock.Arguments) { <-block }).Return(nil, io.EOF)

	err := s.Hold(ms)
	close(block)

	assert.Error(t, err)
	assert.ErrorContains(t, err, "tether missed heartbeats")

	select {
	case r := <-reasons:
		assert.Equal(t, "tether missed heartbeats", r)
	case <-time.After(time.Second):
		t.Fatal("OnExpire not called")
	}

	ms.AssertExpectations(t)
}

func TestHold_PulsesStop(t *testing.T) {
	s, reasons := newTestTether(30 * time.Millisecond)

	ms := tethermocks.NewTetherHoldServer(t)
	block := make(chan struct{})

	ms.On("Recv").Return(nil, nil).Times(3)
	ms.On("Send", mock.Anything).Return(nil).Times(3)
	ms.On("Recv").Run(func(mock.Arguments) { <-block }).Return(nil, io.EOF)

	err := s.Hold(ms)
	close(block)

	assert.Error(t, err)
	assert.ErrorContains(t, err, "tether missed heartbeats")

	select {
	case r := <-reasons:
		assert.Equal(t, "tether missed heartbeats", r)
	case <-time.After(time.Second):
		t.Fatal("OnExpire not called")
	}

	ms.AssertExpectations(t)
}

func TestHold_AlreadyTethered(t *testing.T) {
	s, reasons := newTestTether(30 * time.Millisecond)
	s.tethered = true

	ms := tethermocks.NewTetherHoldServer(t)

	err := s.Hold(ms)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "controller already tethered")

	select {
	case <-reasons:
		t.Fatal("OnExpire should not be called for admission failure")
	default:
	}

	ms.AssertExpectations(t)
}
