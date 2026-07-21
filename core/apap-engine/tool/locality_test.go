// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngineLocalityResolverCachesHostAndCleansUpOnce(t *testing.T) {
	targetCleanupCalls := atomic.Int32{}
	hostCleanupCalls := atomic.Int32{}
	resolveHostCalls := atomic.Int32{}
	hostEngine := &AgentEngine{}

	resolve, cleanup := NewEngineLocalityResolver(
		EngineLocality{
			Engine:    &AgentEngine{},
			ToolsRoot: "/target/tools",
		},
		func() { targetCleanupCalls.Add(1) },
		func() (EngineLocality, func(), error) {
			resolveHostCalls.Add(1)
			return EngineLocality{
				Engine:    hostEngine,
				ToolsRoot: "/host/tools",
			}, func() { hostCleanupCalls.Add(1) }, nil
		},
	)

	first, err := resolve("host")
	require.NoError(t, err)
	second, err := resolve("host")
	require.NoError(t, err)

	assert.Same(t, hostEngine, first.Engine)
	assert.Same(t, hostEngine, second.Engine)
	assert.Equal(t, "/host/tools", first.ToolsRoot)
	assert.Equal(t, int32(1), resolveHostCalls.Load())

	cleanup()
	cleanup()

	assert.Equal(t, int32(1), targetCleanupCalls.Load())
	assert.Equal(t, int32(1), hostCleanupCalls.Load())
}

func TestEngineLocalityResolverResolvesTargetAndRejectsUnknown(t *testing.T) {
	targetEngine := &AgentEngine{}
	resolve, cleanup := NewEngineLocalityResolver(
		EngineLocality{
			Engine:    targetEngine,
			ToolsRoot: "/target/tools",
		},
		nil,
		func() (EngineLocality, func(), error) {
			return EngineLocality{}, nil, errors.New("should not be called")
		},
	)
	defer cleanup()

	targetLocality, err := resolve("target")
	require.NoError(t, err)
	assert.Same(t, targetEngine, targetLocality.Engine)
	assert.Equal(t, "/target/tools", targetLocality.ToolsRoot)

	_, err = resolve("unknown")
	require.ErrorContains(t, err, `unsupported engine locality "unknown"`)
}
