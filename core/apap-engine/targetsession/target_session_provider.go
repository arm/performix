// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package targetsession

import (
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/locality"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
)

// NewTargetSessionProvider creates a new default TargetSessionProvider.
func NewTargetSessionProvider(toolsDir string, rootWorkerEnabled bool) TargetSessionProvider {
	return &targetSessionProvider{
		toolsDir:               toolsDir,
		agentConnectionCreator: agent.NewConnectionCreator(toolsDir, rootWorkerEnabled),
	}
}

// TargetSessionProvider creates and caches target sessions.
type TargetSessionProvider interface {
	// TargetSession returns the TargetSession for the given target.
	TargetSession(target target.Target) (TargetSession, error)
	// HostSession returns the TargetSession for host-side operations.
	HostSession() (TargetSession, error)
	// Shutdown shuts down all target sessions and stops new sessions from being created.
	Shutdown() error
}

// targetSessionProvider implements TargetSessionProvider.
type targetSessionProvider struct {
	mu                     sync.Mutex
	closed                 bool
	entries                []*targetSession
	toolsDir               string
	agentConnectionCreator agent.ConnectionCreator
}

func (tsp *targetSessionProvider) TargetSession(target target.Target) (TargetSession, error) {
	tsp.mu.Lock()
	defer tsp.mu.Unlock()
	entry, err := tsp.session(target)
	if err != nil {
		return nil, err
	}
	return &localityScopedTargetSession{base: entry, localityName: locality.Target}, nil
}

func (tsp *targetSessionProvider) HostSession() (TargetSession, error) {
	tsp.mu.Lock()
	defer tsp.mu.Unlock()
	entry, err := tsp.session(&target.LocalTarget{})
	if err != nil {
		return nil, err
	}
	return &localityScopedTargetSession{base: entry, localityName: locality.Host}, nil
}

func (tsp *targetSessionProvider) session(target target.Target) (*targetSession, error) {
	if tsp.closed {
		return nil, message.New(message.EngineTargetSessionShuttingDown)
	}
	for _, entry := range tsp.entries {
		if reflect.DeepEqual(entry.target, target) {
			return entry, nil
		}
	}
	newSession := newTargetSession(target, tsp.agentConnectionCreator, tsp.toolsDir)
	tsp.entries = append(tsp.entries, newSession)
	return newSession, nil
}

func (tsp *targetSessionProvider) Shutdown() error {
	tsp.mu.Lock()
	defer tsp.mu.Unlock()
	tsp.closed = true
	var joined error
	for _, ts := range tsp.entries {
		if err := ts.Close(); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	if joined == nil {
		return nil
	}
	return fmt.Errorf("error closing target connections: %w", joined)
}
