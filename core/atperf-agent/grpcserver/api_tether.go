// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"errors"
	"io"
	"sync"
	"time"

	"github.com/Arm-Debug/apap-cli/clients/go/tetherproto"
)

const defaultTetherDeadline = 180 * time.Second

type TetherServer struct {
	tetherproto.UnimplementedTetherServer

	Deadline time.Duration
	OnExpire func(reason string)

	mu       sync.Mutex
	tethered bool
}

func NewTetherServer(deadline time.Duration, onExpire func(string)) *TetherServer {
	return &TetherServer{Deadline: deadline, OnExpire: onExpire}
}

func (s *TetherServer) Hold(stream tetherproto.Tether_HoldServer) error {
	s.mu.Lock()
	if s.tethered {
		s.mu.Unlock()
		return errors.New("controller already tethered")
	}
	s.tethered = true
	s.mu.Unlock()

	deadline := s.Deadline
	if deadline <= 0 {
		deadline = defaultTetherDeadline
	}

	pulse := make(chan struct{}, 1)
	errc := make(chan error, 1)

	go func() {
		for {
			_, err := stream.Recv()
			if err != nil {
				errc <- err
				return
			}
			if err := stream.Send(&tetherproto.PulseAck{}); err != nil {
				errc <- err
				return
			}
			select {
			case pulse <- struct{}{}:
			default:
			}
		}
	}()

	timer := time.NewTimer(deadline)
	defer timer.Stop()

	msg := "tether error"
	defer func() {
		if s.OnExpire != nil {
			s.OnExpire(msg)
		}
	}()

	for {
		select {
		// Timer expired: no pulse in time.
		case <-timer.C:
			msg = "tether missed heartbeats"
			return errors.New(msg)
		// Recv() ended: either controller closed or an error.
		case err := <-errc:
			if errors.Is(err, io.EOF) {
				msg = "tether closed by controller"
				return nil
			}
			msg = "tether receive error: " + err.Error()
			return errors.New(msg)
		// Pulse received: extend the deadline.
		case <-pulse:
			timer.Reset(deadline)
		}
	}
}
