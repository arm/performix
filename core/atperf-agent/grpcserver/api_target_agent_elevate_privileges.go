// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"fmt"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/atperf-agent/privilege"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

type PrivilegeProofMech int

const (
	NoPasswdUserns PrivilegeProofMech = iota
	NoPasswdSudo
	SudoPassword
	SetuidHelper
)

type ElevatorConfig struct {
	Pm                process.ProcessManager
	AcceptorFactory   privilege.AcceptorFactory
	RootWorkerFactory privilege.RootWorkerProcessFactory
	Mech              PrivilegeProofMech
	Logger            *log.Logger
}

func (c ElevatorConfig) getRootWorkerFactory() privilege.RootWorkerProcessFactory {
	if c.RootWorkerFactory != nil {
		return c.RootWorkerFactory
	}
	return privilege.NewRootWorkerProcess
}

// Elevator manages privilege elevation by spawning a root worker
// and then having a gRPC client connection back to it.
type Elevator struct {
	rootWorker privilege.RootWorkerProcess
	client     targetagentproto.TargetAgentClient
	mu         sync.Mutex
}

// RootWorkerClient returns the currently active root worker gRPC client if one exists.
func (a *Elevator) RootWorkerClient() (targetagentproto.TargetAgentClient, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.client == nil {
		return nil, false
	}
	return a.client, true
}

// ElevatePrivileges elevates the agent's privileges by spawning a root worker proccess
// with the given proof mechanism. If a root worker exist, this is a no-op.
// Currently only NoPasswdSudo (passwordless sudo) is supported.
func (a *Elevator) ElevatePrivileges(ctx context.Context, ec ElevatorConfig) error {
	// Check mechanism support
	switch ec.Mech {
	case NoPasswdSudo:
		log.Info("Attempting to elevating privileges with mechanism: passwordless sudo")
	case NoPasswdUserns:
		log.Warn("Privilege elevation requested with unsupported proof mechanism: no-passwd user namespace")
		return message.New(message.AgentElevatePrivilegesProofMechanismNotSupported).
			WithMetadata(map[string]string{"mech": "NoPasswdUserns"})
	case SudoPassword:
		log.Warn("Privilege elevation requested with unsupported proof mechanism: sudo password")
		return message.New(message.AgentElevatePrivilegesProofMechanismNotSupported).
			WithMetadata(map[string]string{"mech": "SudoPassword"})
	case SetuidHelper:
		log.Warn("Privilege elevation requested with unsupported proof mechanism: setuid helper")
		return message.New(message.AgentElevatePrivilegesProofMechanismNotSupported).
			WithMetadata(map[string]string{"mech": "SetuidHelper"})
	default:
		log.Warn("Privilege elevation requested with unknown proof mechanism")
		return message.New(message.AgentElevatePrivilegesProofMechanismUnknown)
	}

	// NoPasswdSudo
	rootWorker, err := a.createAndSetNewRootWorker(ctx, ec)
	if err != nil {
		log.WithError(err).Debug("Privilege elevation failed: could not create root worker")
		return message.New(message.AgentElevatePrivilegesMechanismPasswordlessSudo).
			WithCause(err)
	}

	if rootWorker == nil {
		log.Debug("Privilege elevation did not need to a new root worker: already have one")
		return nil
	}

	// Start goroutines after successfully creating and setting the root worker. These both cleanup on error,
	// but use the returned rootWorker instance to avoid race conditions in the case that subsequent calls to
	// ElevatePrivileges are made. In such a case we should still cleanup the root worker we just created, but not a
	// newer one.
	go func() {
		err := rootWorker.StreamLogs(log.StandardLogger())
		if err != nil {
			a.cleanupRootWorkerAndUnsetIfCurrent(rootWorker)
		}
	}()

	rootWorker.StartWatchdog(func() {
		a.cleanupRootWorkerAndUnsetIfCurrent(rootWorker)
	})

	return nil
}

func (a *Elevator) createAndSetNewRootWorker(ctx context.Context, ec ElevatorConfig) (privilege.RootWorkerProcess, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.rootWorker != nil {
		return nil, nil
	}

	// Error to return on failure
	rootWorkerErr := message.New(message.AgentElevatePrivilegesRootWorkerStartFailed)

	factory := ec.getRootWorkerFactory()
	rootWorker, err := factory(
		ec.Pm,
		ec.AcceptorFactory,
		privilege.RootWorkerProcessConfig{TransportLoggingEnabled: false},
	)
	if err != nil {
		log.WithError(err).Error("Failed to create root worker process")
		return nil, rootWorkerErr.WithCause(err)
	}

	client, err := rootWorker.Launch(ctx)
	if err != nil {
		log.WithError(err).Error("Failed to launch root worker process")
		return nil, rootWorkerErr.WithCause(err)
	}

	if err := rootWorker.LogVersion(ctx); err != nil {
		log.WithError(err).Error("Failed to log root worker version")
		a.cleanupRootWorkerInstance(rootWorker)
		return nil, rootWorkerErr.WithCause(err)
	}

	if privileged, err := rootWorker.CheckPrivileges(ctx); err != nil {
		log.WithError(err).Error("Failed to check root worker privileges")
		a.cleanupRootWorkerInstance(rootWorker)
		return nil, rootWorkerErr.WithCause(err)
	} else if !privileged {
		err := fmt.Errorf("root worker does not have elevated privileges")
		log.WithError(err).Error("Root worker privilege check failed")
		a.cleanupRootWorkerInstance(rootWorker)
		return nil, rootWorkerErr.WithCause(err)
	}

	log.Info("Root worker process launched successfully")

	a.rootWorker = rootWorker
	a.client = client

	// Also return it to the caller, so they can be sure they have a reference to the root worker just created.
	// This is important as we release the lock before returning.
	return rootWorker, nil
}

func (a *Elevator) CleanupRootWorker() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.cleanupRootWorkerInstance(a.rootWorker)
	a.rootWorker = nil
	a.client = nil
}

// cleanupRootWorkerAndUnsetIfCurrent cleans up the supplied root worker but only clears the API instance if it is the
// same as the provided instance.
func (a *Elevator) cleanupRootWorkerAndUnsetIfCurrent(rw privilege.RootWorkerProcess) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.cleanupRootWorkerInstance(rw)
	if a.rootWorker == rw {
		a.rootWorker = nil
		a.client = nil
	}
}

func (a *Elevator) cleanupRootWorkerInstance(rw privilege.RootWorkerProcess) {
	if rw == nil {
		return
	}
	rw.Close()
}
