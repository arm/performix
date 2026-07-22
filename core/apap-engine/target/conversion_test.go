// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngineTargetFromJSON(t *testing.T) {
	t.Run("successfully converts ssh target", func(t *testing.T) {
		in := JSONTarget{
			Value: &JSONSSHTarget{
				Jumps: []JSONSSHHostConfig{{}, {}, {}},
			},
		}

		out, err := EngineTargetFromJSON(in)
		require.NoError(t, err)

		st, ok := out.(*SSHTarget)
		require.True(t, ok)

		assert.Len(t, st.Jumps, 3)
	})

	t.Run("successfully converts local target", func(t *testing.T) {
		in := JSONTarget{Value: &JSONLocalTarget{}}

		out, err := EngineTargetFromJSON(in)
		require.NoError(t, err)

		_, ok := out.(*LocalTarget)
		require.True(t, ok)
	})

	t.Run("successfully converts android target", func(t *testing.T) {
		deviceIP := "android-target.invalid:5555"
		in := JSONTarget{Value: &JSONAndroidTarget{SerialNumber: "device-123", DeviceIPAddress: &deviceIP}}

		out, err := EngineTargetFromJSON(in)
		require.NoError(t, err)

		androidTarget, ok := out.(*AndroidTarget)
		require.True(t, ok)
		assert.Equal(t, "device-123", androidTarget.SerialNumber)
		require.NotNil(t, androidTarget.DeviceIPAddress)
		assert.Equal(t, deviceIP, *androidTarget.DeviceIPAddress)
	})

	t.Run("fails on unknown target", func(t *testing.T) {
		in := JSONTarget{}

		_, err := EngineTargetFromJSON(in)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown target type")
	})
}

func TestJSONTargetFromEngine(t *testing.T) {
	t.Run("successfully converts ssh target", func(t *testing.T) {
		in := &SSHTarget{
			Jumps: []SSHHostConfig{{}, {}, {}},
		}

		jt, err := JSONTargetFromEngine(in)
		require.NoError(t, err)

		out, ok := jt.Value.(*JSONSSHTarget)
		require.True(t, ok)

		assert.Len(t, out.Jumps, 3)
	})

	t.Run("successfully converts local target", func(t *testing.T) {
		in := &LocalTarget{}

		jt, err := JSONTargetFromEngine(in)
		require.NoError(t, err)

		_, ok := jt.Value.(*JSONLocalTarget)
		require.True(t, ok)
	})

	t.Run("successfully converts android target", func(t *testing.T) {
		deviceIP := "android-target.invalid:5555"
		in := &AndroidTarget{SerialNumber: "device-123", DeviceIPAddress: &deviceIP}

		jt, err := JSONTargetFromEngine(in)
		require.NoError(t, err)

		out, ok := jt.Value.(*JSONAndroidTarget)
		require.True(t, ok)
		assert.Equal(t, "device-123", out.SerialNumber)
		require.NotNil(t, out.DeviceIPAddress)
		assert.Equal(t, deviceIP, *out.DeviceIPAddress)
	})

	t.Run("fails on unknown target", func(t *testing.T) {
		var in Target

		_, err := JSONTargetFromEngine(in)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown target type")
	})
}
