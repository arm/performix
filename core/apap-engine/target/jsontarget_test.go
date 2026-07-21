// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var jsonDataSSHTarget = []byte(`{
			"type": "ssh",
			"value": {
				"jumps": [
					{
						"host": "192.168.1.1",
						"port": 22,
						"username": "whoever",
						"private_key_filename": "/foo/bar/key",
						"host_key_policy": "strict",
						"authentication_method": "key"
					},
					{
						"host": "192.168.1.2",
						"port": 2222,
						"username": "whoever",
						"private_key_filename": "/foo/key",
						"host_key_policy": "ignore",
						"authentication_method": "key"
					},
					{
						"host": "192.168.1.3",
						"port": 22,
						"username": "whoever",
						"private_key_filename": "/foo/pass",
						"host_key_policy": "strict",
						"authentication_method": "password"
					}
				]
			}
		}`)

var jsonDataLocalTarget = []byte(`{
			"type": "local",
			"value": {}
		}`)

var goDataSSHTarget = JSONSSHTarget{
	Jumps: []JSONSSHHostConfig{
		{
			Host:               "192.168.1.1",
			Port:               22,
			Username:           "whoever",
			PrivateKeyFilename: "/foo/bar/key",
			HostKeyPolicy:      RejectHostKeyIfMissing,
			AuthMethod:         SSHAuthMethodKey,
		},
		{
			Host:               "192.168.1.2",
			Port:               2222,
			Username:           "whoever",
			PrivateKeyFilename: "/foo/key",
			HostKeyPolicy:      IgnoreHostKey,
			AuthMethod:         SSHAuthMethodKey,
		},
		{
			Host:               "192.168.1.3",
			Port:               22,
			Username:           "whoever",
			PrivateKeyFilename: "/foo/pass",
			HostKeyPolicy:      RejectHostKeyIfMissing,
			AuthMethod:         SSHAuthMethodPassword,
		},
	},
}

var goDataLocalTarget = JSONLocalTarget{}

func TestJSONSSHTarget_Type(t *testing.T) {
	assert.Equal(t, TargetTypeSSH, goDataSSHTarget.Type())
}

func TestJSONLocalTarget_Type(t *testing.T) {
	assert.Equal(t, TargetTypeLocal, goDataLocalTarget.Type())
}

func TestJSONTarget_UnmarshalJSON(t *testing.T) {
	t.Run("successfully unmarshals SSH target", func(t *testing.T) {
		healthyJSON := make([]byte, len(jsonDataSSHTarget))
		copy(healthyJSON, jsonDataSSHTarget)

		var tgt JSONTarget
		err := tgt.UnmarshalJSON(healthyJSON)
		require.NoError(t, err)

		sshTarget, ok := tgt.Value.(*JSONSSHTarget)
		require.True(t, ok)

		assert.Equal(t, goDataSSHTarget, *sshTarget)
	})

	t.Run("successfully unmarshals local target", func(t *testing.T) {
		healthyJSON := make([]byte, len(jsonDataLocalTarget))
		copy(healthyJSON, jsonDataLocalTarget)

		var tgt JSONTarget
		err := tgt.UnmarshalJSON(healthyJSON)
		require.NoError(t, err)

		localTarget, ok := tgt.Value.(*JSONLocalTarget)
		require.True(t, ok)

		assert.Equal(t, goDataLocalTarget, *localTarget)
	})

	t.Run("defaults to key auth when authentication method missing", func(t *testing.T) {
		jsonData := []byte(`{
			"type": "ssh",
			"value": {
				"jumps": [
					{
						"host": "192.168.1.11",
						"port": 22,
						"username": "whoever",
						"private_key_filename": "/foo/bar/key",
						"host_key_policy": "strict"
					}
				]
			}
		}`)

		var tgt JSONTarget
		err := tgt.UnmarshalJSON(jsonData)
		require.NoError(t, err)

		sshTarget, ok := tgt.Value.(*JSONSSHTarget)
		require.True(t, ok)

		require.Len(t, sshTarget.Jumps, 1)
		assert.Equal(t, SSHAuthMethodKey, sshTarget.Jumps[0].AuthMethod)
	})

	t.Run("successfully unmarshals legacy v1 target", func(t *testing.T) {
		legacyV1JSON := []byte(`{
			"host": "192.168.1.100",
			"port": 22,
			"user": "whoever",
			"key": "/foo/bar/key"
		}`)

		var tgt JSONTarget
		err := tgt.UnmarshalJSON(legacyV1JSON)
		require.NoError(t, err)

		sshTarget, ok := tgt.Value.(*JSONSSHTarget)
		require.True(t, ok)

		expectedTarget := JSONSSHTarget{
			Jumps: []JSONSSHHostConfig{
				{
					Host:               "192.168.1.100",
					Port:               22,
					Username:           "whoever",
					PrivateKeyFilename: "/foo/bar/key",
					HostKeyPolicy:      RejectHostKeyIfMissing,
					AuthMethod:         SSHAuthMethodKey,
				},
			},
		}

		assert.Equal(t, expectedTarget, *sshTarget)
	})

	t.Run("fails on bad JSON", func(t *testing.T) {
		badJSON := []byte(`{ bad JSON }`)

		var tgt JSONTarget
		err := tgt.UnmarshalJSON(badJSON)
		assert.Error(t, err)
	})

	t.Run("fails on unknown type", func(t *testing.T) {
		unknownTypeJSON := []byte(`{
			"type": "unknown",
			"value": {}
		}`)

		var tgt JSONTarget
		err := tgt.UnmarshalJSON(unknownTypeJSON)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unrecognized target type")
	})
}

func TestJSONTarget_MarshalJSON(t *testing.T) {
	t.Run("successfully marshals SSH target", func(t *testing.T) {
		target := JSONTarget{
			Value: &goDataSSHTarget,
		}

		data, err := target.MarshalJSON()
		require.NoError(t, err)

		assert.JSONEq(t, string(jsonDataSSHTarget), string(data))
	})

	t.Run("successfully marshals local target", func(t *testing.T) {
		target := JSONTarget{
			Value: &goDataLocalTarget,
		}

		data, err := target.MarshalJSON()
		require.NoError(t, err)

		assert.JSONEq(t, string(jsonDataLocalTarget), string(data))
	})
}
