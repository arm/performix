// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/benbjohnson/clock"
	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/tetherproto"
)

const (
	defaultPulseInterval = 20 * time.Second
	defaultAckTimeout    = 30 * time.Second
	defaultMaxMissedAcks = 5
)

var (
	errTetherStreamFailure = errors.New("tether stream failure")
	errTetherAckTimeout    = errors.New("tether ACK timeout")
)

type AgentTether interface {
	Start() error
	OnTransportFailure(func(error))
	Close()
}

type agentTether struct {
	tether        tetherproto.TetherClient
	cancel        context.CancelFunc
	clk           clock.Clock
	logger        *log.Logger
	pulseEvery    time.Duration
	ackTimeout    time.Duration
	maxMissedAcks int
	onDisconnect  func(error)
}

func newAgentTether(tether tetherproto.TetherClient, pulseEvery time.Duration) AgentTether {
	return &agentTether{
		tether:        tether,
		clk:           clock.New(),
		logger:        log.StandardLogger(),
		pulseEvery:    pulseEvery,
		ackTimeout:    defaultAckTimeout,
		maxMissedAcks: defaultMaxMissedAcks,
	}
}

func (t *agentTether) OnTransportFailure(fn func(error)) {
	t.onDisconnect = fn
}

// Start opens a bidirectional streaming RPC to the agent and sends
// PulseRequest messages on a ticker while listening for ACKs.
// The agent will shut itself down if the stream ends or if a pulse
// doesn't arrive before its deadline. The deadline is reset with
// every pulse.
// If the engine reaches the configured number of missed ACK deadlines, or the
// stream errors, the transport failure handler is invoked. Any ACK resets the
// miss count.
// If pulseEvery <= 0, default to defaultPulseInterval.
func (t *agentTether) Start() error {
	if t.tether == nil {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	stream, err := t.tether.Hold(ctx)
	if err != nil {
		cancel()
		t.logger.Errorf("could not start tether to new agent connection: %v", err)
		return message.New(message.CommonUnknownError).WithCause(fmt.Errorf("could not start tether to new agent connection: %w", err))
	}

	t.cancel = cancel

	pulseEvery := t.pulseEvery
	if pulseEvery <= 0 {
		pulseEvery = defaultPulseInterval
	}

	disconnectOnce := sync.Once{}

	signalDisconnect := func(err error) {
		disconnectOnce.Do(func() {
			if t.onDisconnect != nil {
				t.onDisconnect(err)
			}
			cancel()
		})
	}

	// Send an initial pulse to start the ACK timer.
	if err := stream.Send(&tetherproto.PulseRequest{}); err != nil {
		cancel()
		t.logger.Errorf("could not send initial tether pulse: %v", err)
		return message.New(message.CommonUnknownError).WithCause(fmt.Errorf("could not send initial tether pulse: %w", err))
	}

	// Engine -> Agent Tether: Pulse Loop
	pulseTicker := t.clk.Ticker(pulseEvery)
	go func() {
		defer pulseTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				_ = stream.CloseSend()
				return
			case <-pulseTicker.C:
				if err := stream.Send(&tetherproto.PulseRequest{}); err != nil {
					signalDisconnect(fmt.Errorf("%w while sending pulse: %w", errTetherStreamFailure, err))
					return
				}
			}
		}
	}()

	// Agent -> Engine Tether: ACK Loop
	ackTimer := t.clk.Timer(t.ackTimeout)
	go func() {
		ackCh := make(chan struct{}, 1)
		recvErrCh := make(chan error, 1)

		// Start a goroutine to receive ACKs from the agent.
		go func() {
			for {
				_, err := stream.Recv()
				if err != nil {
					select {
					case recvErrCh <- err:
					case <-ctx.Done():
					}
					return
				}

				// Coalesce bursts of ACKs; one is enough to reset the timer.
				select {
				case ackCh <- struct{}{}:
				default:
				}
			}
		}()

		defer ackTimer.Stop()
		missedAcks := 0

		// Loop until the context is cancelled, an error is received from the stream, or the timer expires.
		for {
			select {
			case <-ctx.Done():
				return
			case err := <-recvErrCh:
				if ctx.Err() != nil {
					return
				}
				signalDisconnect(fmt.Errorf("%w while receiving ACK: %w", errTetherStreamFailure, err))
				return
			case <-ackCh:
				missedAcks = 0
				if !ackTimer.Stop() {
					select {
					case <-ackTimer.C:
					default:
					}
				}
				ackTimer.Reset(t.ackTimeout)
			case <-ackTimer.C:
				// Re-check for a queued ACK to avoid false disconnects on deadline races.
				select {
				case <-ackCh:
					missedAcks = 0
					ackTimer.Reset(t.ackTimeout)
					continue
				default:
				}

				missedAcks++
				entry := t.logger.WithFields(log.Fields{
					"missed_acks":     missedAcks,
					"max_missed_acks": t.maxMissedAcks,
				})
				if missedAcks >= t.maxMissedAcks {
					entry.Error("Agent tether ACK deadline missed")
					signalDisconnect(fmt.Errorf("%w: missed %d ACK deadlines", errTetherAckTimeout, missedAcks))
					return
				}
				entry.Info("Agent tether ACK deadline missed")
				ackTimer.Reset(t.ackTimeout)
			}
		}
	}()

	return nil
}

// Close cancels the tether context.
func (t *agentTether) Close() {
	if t.cancel != nil {
		t.cancel()
	}
}
