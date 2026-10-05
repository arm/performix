// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCPUTimeProcessorReadsCompressedProfilingState(t *testing.T) {
	component := writeCompressedComponent(t, "state.xml", []byte(
		`<state stop_time="191015855589" time_unit="nanoseconds"/>`,
	))

	duration, err := (&CPUTimeProcessor{}).getProfilingDurationInMS(component)

	require.NoError(t, err)
	assert.InDelta(t, 191015.855589, duration, 1e-9)
}
