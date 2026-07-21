// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	targetagentmocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/tetherproto"
)

func newTestAgentTether(
	client tetherproto.TetherClient,
	pulseEvery time.Duration,
) (*agentTether, *clock.Mock) {
	mockClock := clock.NewMock()
	tether := newAgentTether(client, pulseEvery).(*agentTether)
	tether.clk = mockClock
	return tether, mockClock
}

func newTestLogger() (*log.Logger, *logtest.Hook) {
	logger := log.New()
	logger.SetOutput(io.Discard)
	return logger, logtest.NewLocal(logger)
}

func TestStart_SendsAndCloses(t *testing.T) {
	sentPulse := make(chan struct{}, 1)
	closeSendCalled := make(chan struct{}, 1)
	block := make(chan struct{})
	defer close(block)

	stream := &targetagentmocks.TetherHoldClient{}
	stream.On("Send", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		select {
		case sentPulse <- struct{}{}:
		default:
		}
	}).Maybe()
	stream.On("Recv").Run(func(mock.Arguments) { <-block }).Return(&tetherproto.PulseAck{}, io.EOF).Maybe()
	stream.On("CloseSend").Run(func(mock.Arguments) {
		closeSendCalled <- struct{}{}
	}).Return(nil).Once()

	tether := &targetagentmocks.TetherClient{}
	tether.On("Hold", mock.Anything, mock.Anything).Return(stream, nil).Once()

	at, _ := newTestAgentTether(tether, time.Hour)
	err := at.Start()
	assert.NoError(t, err)

	select {
	case <-sentPulse:
	default:
		t.Fatal("initial pulse was not sent synchronously")
	}

	// Cancel and wait for the pulse loop to close its side of the stream.
	at.Close()
	select {
	case <-closeSendCalled:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("tether did not close the send stream")
	}

	// Second close doesn't call CloseSend
	at.Close()

	tether.AssertExpectations(t)
	stream.AssertExpectations(t)
}

func TestStart_NoClient(t *testing.T) {
	at := newAgentTether(nil, 10*time.Millisecond)
	assert.NoError(t, at.Start())
	at.Close()
}

func TestStart_HoldError(t *testing.T) {
	tether := &targetagentmocks.TetherClient{}
	tether.
		On("Hold", mock.Anything, mock.Anything).
		Return((*targetagentmocks.TetherHoldClient)(nil), assert.AnError).
		Once()

	at := newAgentTether(tether, 10*time.Millisecond)
	err := at.Start()
	var msg message.Message
	assert.True(t, errors.As(err, &msg))
	assert.Equal(t, message.CommonUnknownError, msg.Code())
	assert.ErrorContains(t, msg.Unwrap(), "could not start tether")

	at.Close()

	tether.AssertExpectations(t)
}

func TestStart_FailedInitialPulse(t *testing.T) {
	stream := &targetagentmocks.TetherHoldClient{}
	stream.On("Send", mock.Anything).Return(assert.AnError).Once()
	stream.On("CloseSend").Return(nil).Once()

	tether := &targetagentmocks.TetherClient{}
	tether.On("Hold", mock.Anything, mock.Anything).Return(stream, nil).Once()

	at := newAgentTether(tether, 10*time.Millisecond)
	err := at.Start()
	var msg message.Message
	assert.True(t, errors.As(err, &msg))
	assert.Equal(t, message.CommonUnknownError, msg.Code())
	assert.ErrorContains(t, msg.Unwrap(), "could not send initial tether pulse")

	at.Close()
}

func TestStart_ClassifiesPulseSendFailure(t *testing.T) {
	recvBlock := make(chan struct{})
	stream := &targetagentmocks.TetherHoldClient{}
	stream.On("Send", mock.Anything).Return(nil).Once()
	stream.On("Send", mock.Anything).Return(assert.AnError).Once()
	stream.On("Recv").Run(func(mock.Arguments) { <-recvBlock }).Return(nil, io.EOF).Once()
	stream.On("CloseSend").Return(nil).Maybe()

	tether := &targetagentmocks.TetherClient{}
	tether.On("Hold", mock.Anything, mock.Anything).Return(stream, nil).Once()

	at, mockClock := newTestAgentTether(tether, time.Second)
	disconnected := make(chan error, 1)
	at.OnTransportFailure(func(err error) { disconnected <- err })

	require.NoError(t, at.Start())
	defer at.Close()
	defer close(recvBlock)
	mockClock.Add(time.Second)

	select {
	case err := <-disconnected:
		assert.ErrorIs(t, err, errTetherStreamFailure)
		assert.ErrorIs(t, err, assert.AnError)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("tether did not report the pulse send failure")
	}

	tether.AssertExpectations(t)
	stream.AssertExpectations(t)
}

func TestStart_ClassifiesAckReceiveFailure(t *testing.T) {
	stream := &targetagentmocks.TetherHoldClient{}
	stream.On("Send", mock.Anything).Return(nil).Once()
	stream.On("Recv").Return(nil, assert.AnError).Once()
	stream.On("CloseSend").Return(nil).Maybe()

	tether := &targetagentmocks.TetherClient{}
	tether.On("Hold", mock.Anything, mock.Anything).Return(stream, nil).Once()

	at := newAgentTether(tether, time.Hour)
	disconnected := make(chan error, 1)
	at.OnTransportFailure(func(err error) { disconnected <- err })

	require.NoError(t, at.Start())
	defer at.Close()

	select {
	case err := <-disconnected:
		assert.ErrorIs(t, err, errTetherStreamFailure)
		assert.ErrorIs(t, err, assert.AnError)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("tether did not report the ACK receive failure")
	}

	tether.AssertExpectations(t)
	stream.AssertExpectations(t)
}

func TestStart_DisconnectsAfterMaxMissedAcks(t *testing.T) {
	logger, hook := newTestLogger()

	recvBlock := make(chan struct{})
	stream := &targetagentmocks.TetherHoldClient{}
	stream.On("Send", mock.Anything).Return(nil).Once()
	stream.On("Recv").Run(func(mock.Arguments) { <-recvBlock }).Return(nil, io.EOF).Once()
	stream.On("CloseSend").Return(nil).Maybe()

	tether := &targetagentmocks.TetherClient{}
	tether.On("Hold", mock.Anything, mock.Anything).Return(stream, nil).Once()

	at, mockClock := newTestAgentTether(tether, time.Hour)
	at.logger = logger
	at.ackTimeout = time.Minute
	disconnected := make(chan error, 1)
	at.OnTransportFailure(func(err error) { disconnected <- err })

	require.NoError(t, at.Start())
	defer close(recvBlock)
	defer at.Close()

	for i := 1; i <= at.maxMissedAcks; i++ {
		mockClock.Add(at.ackTimeout)
		require.Eventually(t, func() bool {
			return len(missedAckLogEntries(hook)) == i
		}, 5*time.Second, time.Millisecond)

		if i < at.maxMissedAcks {
			select {
			case err := <-disconnected:
				t.Fatalf("tether disconnected after %d missed ACKs: %v", i, err)
			default:
			}
		}
	}

	select {
	case err := <-disconnected:
		assert.ErrorIs(t, err, errTetherAckTimeout)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("tether did not disconnect after three missed ACK deadlines")
	}

	missedAckEntries := missedAckLogEntries(hook)
	require.Len(t, missedAckEntries, at.maxMissedAcks)
	for i, entry := range missedAckEntries {
		assert.Equal(t, i+1, entry.Data["missed_acks"])
		assert.Equal(t, at.maxMissedAcks, entry.Data["max_missed_acks"])
	}

	tether.AssertExpectations(t)
	stream.AssertExpectations(t)
}

func TestStart_AckResetsMissedAcks(t *testing.T) {
	logger, hook := newTestLogger()
	releaseAck := make(chan struct{})
	secondRecvStarted := make(chan struct{})
	finishRecv := make(chan struct{})

	stream := &targetagentmocks.TetherHoldClient{}
	stream.On("Send", mock.Anything).Return(nil).Once()
	stream.On("Recv").Run(func(mock.Arguments) { <-releaseAck }).Return(&tetherproto.PulseAck{}, nil).Once()
	stream.On("Recv").Run(func(mock.Arguments) {
		close(secondRecvStarted)
		<-finishRecv
	}).Return(nil, io.EOF).Once()
	stream.On("CloseSend").Return(nil).Maybe()

	tether := &targetagentmocks.TetherClient{}
	tether.On("Hold", mock.Anything, mock.Anything).Return(stream, nil).Once()

	at, mockClock := newTestAgentTether(tether, time.Hour)
	at.logger = logger
	at.ackTimeout = time.Minute
	disconnected := make(chan error, 1)
	at.OnTransportFailure(func(err error) { disconnected <- err })

	require.NoError(t, at.Start())
	defer close(finishRecv)
	defer at.Close()

	mockClock.Add(at.ackTimeout)
	require.Eventually(t, func() bool {
		return len(missedAckLogEntries(hook)) == 1
	}, 5*time.Second, time.Millisecond)
	close(releaseAck)

	select {
	case <-secondRecvStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("tether did not receive the ACK")
	}
	// Let the ACK loop reset its timer before advancing mock time again.
	mockClock.Add(0)

	for i := 1; i <= at.maxMissedAcks; i++ {
		mockClock.Add(at.ackTimeout)
		require.Eventually(t, func() bool {
			return len(missedAckLogEntries(hook)) == i+1
		}, 5*time.Second, time.Millisecond)

		if i < at.maxMissedAcks {
			select {
			case err := <-disconnected:
				t.Fatalf("tether disconnected after %d missed ACKs following an ACK: %v", i, err)
			default:
			}
		}
	}

	select {
	case err := <-disconnected:
		assert.ErrorIs(t, err, errTetherAckTimeout)
		assert.ErrorContains(t, err, fmt.Sprintf("missed %d ACK deadlines", at.maxMissedAcks))
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("tether did not disconnect after %d missed ACK deadlines following an ACK", at.maxMissedAcks)
	}

	tether.AssertExpectations(t)
	stream.AssertExpectations(t)
}

func missedAckLogEntries(hook *logtest.Hook) []*log.Entry {
	var entries []*log.Entry
	for _, entry := range hook.AllEntries() {
		if entry.Message == "Agent tether ACK deadline missed" {
			entries = append(entries, entry)
		}
	}
	return entries
}
