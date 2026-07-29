// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"errors"
	"sync"

	"github.com/Arm-Debug/apap-cli/apap-engine/locality"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

type CopyFromFunc func(sourceLocality string, sourcePath string, destinationPath string) error

type EngineLocality struct {
	Name          string
	Engine        Engine
	FileCollector FileCollector
	ToolsRoot     string
	CopyFrom      CopyFromFunc
}

type EngineLocalityResolver func(name string) (EngineLocality, error)

type engineLocalityResolver struct {
	target        EngineLocality
	targetCleanup func()

	host        *EngineLocality
	hostCleanup func()
	resolveHost func() (EngineLocality, func(), error)

	mu          sync.Mutex
	cleanupOnce sync.Once
}

func (r *engineLocalityResolver) Resolve(name string) (EngineLocality, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch name {
	case locality.Target:
		return r.target, nil
	case locality.Host:
		if r.host != nil {
			return *r.host, nil
		}
		if r.resolveHost == nil {
			return EngineLocality{}, message.New(message.CommonUnknownError).WithCause(errors.New("no host resolver provided"))
		}

		host, cleanup, err := r.resolveHost()
		if err != nil {
			return EngineLocality{}, err
		}
		r.host = &host
		r.hostCleanup = cleanup
		return host, nil
	default:
		return EngineLocality{}, message.New(message.EngineToolLocalityUnsupported).WithMetadata(map[string]string{"locality": name})
	}
}

func (r *engineLocalityResolver) Cleanup() {
	r.cleanupOnce.Do(func() {
		if r.hostCleanup != nil {
			r.hostCleanup()
		}
		if r.targetCleanup != nil {
			r.targetCleanup()
		}
	})
}

func NewEngineLocalityResolver(
	target EngineLocality,
	targetCleanup func(),
	resolveHost func() (EngineLocality, func(), error),
) (EngineLocalityResolver, func()) {
	resolver := &engineLocalityResolver{
		target:        target,
		targetCleanup: targetCleanup,
		resolveHost:   resolveHost,
	}
	return resolver.Resolve, resolver.Cleanup
}
