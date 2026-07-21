// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"encoding/json"
	"fmt"
	"os/user"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func TestSSHHostConfig_DisplayString(t *testing.T) {
	testCases := []struct {
		name     string
		config   SSHHostConfig
		expected string
	}{
		{
			name:     "Default port is used with valid username and host",
			config:   SSHHostConfig{Host: "example.com", Port: defaultSSHPort, Username: "user"},
			expected: "user@example.com",
		},
		{
			name:     "Non-default port is provided",
			config:   SSHHostConfig{Host: "example.com", Port: 2222, Username: "user"},
			expected: "user@example.com:2222",
		},
		{
			name:     "Empty username defaults to '<unknown>'",
			config:   SSHHostConfig{Host: "example.com", Port: defaultSSHPort, Username: ""},
			expected: "<unknown>@example.com",
		},
		{
			name:     "Empty host defaults to '<unknown>'",
			config:   SSHHostConfig{Host: "", Port: defaultSSHPort, Username: "user"},
			expected: "user@<unknown>",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := tc.config.DisplayString()
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestSSHTarget_LastJump(t *testing.T) {
	t.Run("When no jumps are present, LastJump should return default value", func(t *testing.T) {
		target := &SSHTarget{}
		require.Equal(t, SSHHostConfig{}, target.LastJump())
	})

	t.Run("When one jump is present, LastJump returns that jump", func(t *testing.T) {
		config := SSHHostConfig{Host: "example.com", Port: defaultSSHPort, Username: "user"}
		target := &SSHTarget{Jumps: []SSHHostConfig{config}}
		lastJump := target.LastJump()
		require.NotNil(t, lastJump)
		assert.Equal(t, config, lastJump)
	})

	t.Run("When multiple jumps are present, LastJump returns the final jump", func(t *testing.T) {
		config1 := SSHHostConfig{Host: "example.com", Port: defaultSSHPort, Username: "user"}
		config2 := SSHHostConfig{Host: "final.com", Port: 2222, Username: "final"}
		target := &SSHTarget{Jumps: []SSHHostConfig{config1, config2}}
		lastJump := target.LastJump()
		require.NotNil(t, lastJump)
		assert.Equal(t, config2, lastJump)
	})
}

func TestSSHTarget_DisplayHost(t *testing.T) {
	t.Run("When no jumps are present, DisplayHost returns '<unknown>@<unknown>:0'", func(t *testing.T) {
		target := &SSHTarget{}
		assert.Equal(t, "<unknown>@<unknown>:0", target.DisplayHost())
	})

	t.Run("When one jump is present, DisplayHost returns the jump's DisplayString", func(t *testing.T) {
		config := SSHHostConfig{Host: "example.com", Port: defaultSSHPort, Username: "user"}
		target := &SSHTarget{Jumps: []SSHHostConfig{config}}
		expected := config.DisplayString()
		assert.Equal(t, expected, target.DisplayHost())
	})
}

func TestSSHTarget_GetUserDataDirectoryName(t *testing.T) {
	t.Run("When no jumps are present, an error should be returned", func(t *testing.T) {
		target := &SSHTarget{}
		name, err := target.GetUserDataDirectoryName()
		require.Error(t, err)
		assert.Empty(t, name)
	})

	t.Run("When the jump has an empty username, an error should be returned", func(t *testing.T) {
		config := SSHHostConfig{Host: "example.com", Port: defaultSSHPort, Username: ""}
		target := &SSHTarget{Jumps: []SSHHostConfig{config}}
		name, err := target.GetUserDataDirectoryName()
		require.Error(t, err)
		assert.Empty(t, name)
	})

	t.Run("When a valid jump is present, the username is returned", func(t *testing.T) {
		config := SSHHostConfig{Host: "example.com", Port: 2222, Username: "user"}
		target := &SSHTarget{Jumps: []SSHHostConfig{config}}
		name, err := target.GetUserDataDirectoryName()
		require.NoError(t, err)
		assert.Equal(t, "user", name)
	})
}

func TestSSHTarget_String(t *testing.T) {
	t.Run("When no jumps are present, String returns '<unknown>@<unknown>:0'", func(t *testing.T) {
		target := &SSHTarget{}
		assert.Equal(t, "<unknown>@<unknown>:0", target.String())
	})

	t.Run("When one jump is present, String returns the jump's DisplayString", func(t *testing.T) {
		config := SSHHostConfig{Host: "example.com", Port: defaultSSHPort, Username: "user"}
		target := &SSHTarget{Jumps: []SSHHostConfig{config}}
		expected := config.DisplayString()
		assert.Equal(t, expected, target.String())
	})

	t.Run("When two jumps are present, String returns the final jump's DisplayString plus via information", func(t *testing.T) {
		jump1 := SSHHostConfig{Host: "jump.example.com", Port: defaultSSHPort, Username: "jump"}
		jump2 := SSHHostConfig{Host: "final.example.com", Port: 2222, Username: "final"}
		target := &SSHTarget{Jumps: []SSHHostConfig{jump1, jump2}}
		expected := fmt.Sprintf("%s (via %s)", jump2.DisplayString(), jump1.DisplayString())
		assert.Equal(t, expected, target.String())
	})

	t.Run("When multiple jumps are present, String returns the final jump's DisplayString plus via information in reverse order", func(t *testing.T) {
		jump1 := SSHHostConfig{Host: "jump.example.com", Port: defaultSSHPort, Username: "jump"}
		jump2 := SSHHostConfig{Host: "middle.example.com", Port: defaultSSHPort, Username: "middle"}
		jump3 := SSHHostConfig{Host: "final.example.com", Port: 2222, Username: "final"}
		target := &SSHTarget{Jumps: []SSHHostConfig{jump1, jump2, jump3}}
		expected := fmt.Sprintf("%s (via %s, %s)", jump3.DisplayString(), jump2.DisplayString(), jump1.DisplayString())
		assert.Equal(t, expected, target.String())
	})
}

func TestLocalTarget_DisplayHost(t *testing.T) {
	t.Run("LocalTarget DisplayHost returns 'localhost'", func(t *testing.T) {
		localTarget := &LocalTarget{}
		assert.Equal(t, "localhost", localTarget.DisplayHost())
	})
}

func TestLocalTarget_GetUserDataDirectoryName(t *testing.T) {
	t.Run("LocalTarget GetUserDataDirectoryName returns the current OS username", func(t *testing.T) {
		localTarget := &LocalTarget{}
		username, err := localTarget.GetUserDataDirectoryName()
		require.NoError(t, err)
		currentUser, err := user.Current()
		require.NoError(t, err)
		assert.Equal(t, currentUser.Username, username)
	})
}

func TestLocalTarget_String(t *testing.T) {
	t.Run("LocalTarget String returns the same as DisplayHost", func(t *testing.T) {
		localTarget := &LocalTarget{}
		assert.Equal(t, localTarget.DisplayHost(), localTarget.String())
	})
}

func TestAndroidTarget_DisplayHost(t *testing.T) {
	t.Run("returns serial number", func(t *testing.T) {
		androidTarget := &AndroidTarget{SerialNumber: "device-123"}
		assert.Equal(t, "device-123", androidTarget.DisplayHost())
	})

	t.Run("returns unknown when serial number is empty", func(t *testing.T) {
		androidTarget := &AndroidTarget{}
		assert.Equal(t, "<unknown>", androidTarget.DisplayHost())
	})
}

func TestAndroidTarget_GetUserDataDirectoryName(t *testing.T) {
	t.Run("returns serial number", func(t *testing.T) {
		androidTarget := &AndroidTarget{SerialNumber: "device-123"}

		name, err := androidTarget.GetUserDataDirectoryName()

		require.NoError(t, err)
		assert.Equal(t, "device-123", name)
	})

	t.Run("returns error when serial number is empty", func(t *testing.T) {
		androidTarget := &AndroidTarget{}

		name, err := androidTarget.GetUserDataDirectoryName()

		require.Error(t, err)
		assert.Empty(t, name)
		assert.Contains(t, err.Error(), "missing Android serial number")
	})
}

func TestAndroidTarget_String(t *testing.T) {
	t.Run("returns serial number without device IP address", func(t *testing.T) {
		androidTarget := &AndroidTarget{SerialNumber: "device-123"}
		assert.Equal(t, "device-123", androidTarget.String())
	})

	t.Run("returns serial number when device IP address is empty", func(t *testing.T) {
		deviceIP := ""
		androidTarget := &AndroidTarget{SerialNumber: "device-123", DeviceIPAddress: &deviceIP}
		assert.Equal(t, "device-123", androidTarget.String())
	})

	t.Run("includes device IP address when configured", func(t *testing.T) {
		deviceIP := "android-target.invalid:5555"
		androidTarget := &AndroidTarget{SerialNumber: "device-123", DeviceIPAddress: &deviceIP}
		assert.Equal(t, "device-123 (android-target.invalid:5555)", androidTarget.String())
	})
}

func TestAndroidTarget_Validate(t *testing.T) {
	t.Run("valid with serial number", func(t *testing.T) {
		androidTarget := &AndroidTarget{SerialNumber: "device-123"}
		require.NoError(t, androidTarget.Validate("android_target"))
	})

	t.Run("valid with serial number and device IP address", func(t *testing.T) {
		deviceIP := "android-target.invalid:5555"
		androidTarget := &AndroidTarget{SerialNumber: "device-123", DeviceIPAddress: &deviceIP}
		require.NoError(t, androidTarget.Validate("android_target"))
	})

	t.Run("rejects reserved target name", func(t *testing.T) {
		androidTarget := &AndroidTarget{SerialNumber: "device-123"}

		err := androidTarget.Validate(LocalhostName)

		require.Error(t, err)
		assert.ErrorContains(t, err, message.EngineTargetConfigNameReserved)
	})

	t.Run("rejects missing serial number", func(t *testing.T) {
		androidTarget := &AndroidTarget{}

		err := androidTarget.Validate("android_target")

		require.Error(t, err)
		assert.ErrorContains(t, err, message.EngineTargetConfigInvalidHostFormat)
	})

	t.Run("rejects serial number that looks like an adb flag", func(t *testing.T) {
		androidTarget := &AndroidTarget{SerialNumber: "-device-123"}

		err := androidTarget.Validate("android_target")

		require.Error(t, err)
		assert.ErrorContains(t, err, message.EngineTargetConfigInvalidHostFormat)
	})

	t.Run("rejects serial number containing whitespace", func(t *testing.T) {
		androidTarget := &AndroidTarget{SerialNumber: "device 123"}

		err := androidTarget.Validate("android_target")

		require.Error(t, err)
		assert.ErrorContains(t, err, message.EngineTargetConfigInvalidHostFormat)
	})

	t.Run("rejects device IP address that looks like an adb flag", func(t *testing.T) {
		deviceIP := "-android-target.invalid:5555"
		androidTarget := &AndroidTarget{SerialNumber: "device-123", DeviceIPAddress: &deviceIP}

		err := androidTarget.Validate("android_target")

		require.Error(t, err)
		assert.ErrorContains(t, err, message.EngineTargetConfigInvalidHostFormat)
	})

	t.Run("rejects device IP address containing whitespace", func(t *testing.T) {
		deviceIP := "android target.invalid:5555"
		androidTarget := &AndroidTarget{SerialNumber: "device-123", DeviceIPAddress: &deviceIP}

		err := androidTarget.Validate("android_target")

		require.Error(t, err)
		assert.ErrorContains(t, err, message.EngineTargetConfigInvalidHostFormat)
	})

	t.Run("rejects device IP address containing extra colon-delimited fields", func(t *testing.T) {
		deviceIP := "android-target.invalid:5555:extra:field"
		androidTarget := &AndroidTarget{SerialNumber: "device-123", DeviceIPAddress: &deviceIP}

		err := androidTarget.Validate("android_target")

		require.Error(t, err)
		assert.ErrorContains(t, err, message.EngineTargetConfigInvalidHostFormat)
	})
}

func TestUnmarshalJSON_LegacyTargetV1(t *testing.T) {
	legacyJSON := `{
		"host": "legacy.example.com",
		"port": 2200,
		"user": "legacy-user",
		"key": "/home/legacy/.ssh/id_legacy"
	}`

	var tgt JSONTarget
	if err := json.Unmarshal([]byte(legacyJSON), &tgt); err != nil {
		t.Fatalf("failed to unmarshal legacy v1 JSON: %v", err)
	}

	sshTgt, ok := tgt.Value.(*JSONSSHTarget)
	if !ok {
		t.Fatalf("expected *JSONSSHTarget, got %T", tgt.Value)
	}
	if len(sshTgt.Jumps) != 1 {
		t.Fatalf("expected 1 jump, got %d", len(sshTgt.Jumps))
	}

	got := sshTgt.Jumps[0]
	want := JSONSSHHostConfig{
		Host:               "legacy.example.com",
		Port:               2200,
		Username:           "legacy-user",
		PrivateKeyFilename: "/home/legacy/.ssh/id_legacy",
		HostKeyPolicy:      RejectHostKeyIfMissing, // hard-coded by the compat path
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("jump mismatch:\n  got  %+v\n  want %+v", got, want)
	}
}

func TestUnmarshalJSON_CurrentTarget(t *testing.T) {
	currentJSON := `{
		"type": "ssh",
		"value": {
			"jumps": [
				{
					"host": "new.example.com",
					"port": 2222,
					"username": "new-user",
					"private_key_filename": "/home/new/.ssh/id_new",
					"host_key_policy": "ignore"
				}
			]
		}
	}`

	var tgt JSONTarget
	if err := json.Unmarshal([]byte(currentJSON), &tgt); err != nil {
		t.Fatalf("failed to unmarshal current JSON: %v", err)
	}

	sshTgt, ok := tgt.Value.(*JSONSSHTarget)
	if !ok {
		t.Fatalf("expected *JSONSSHTarget, got %T", tgt.Value)
	}
	if len(sshTgt.Jumps) != 1 {
		t.Fatalf("expected 1 jump, got %d", len(sshTgt.Jumps))
	}

	got := sshTgt.Jumps[0]
	want := JSONSSHHostConfig{
		Host:               "new.example.com",
		Port:               2222,
		Username:           "new-user",
		PrivateKeyFilename: "/home/new/.ssh/id_new",
		HostKeyPolicy:      IgnoreHostKey,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("jump mismatch:\n  got  %+v\n  want %+v", got, want)
	}
}

func TestSSHAuthMethod_JSON(t *testing.T) {
	t.Run("UnmarshalJSON", func(t *testing.T) {
		testCases := []struct {
			name      string
			jsonInput string
			expected  SSHAuthMethod
			expectErr bool
		}{
			{
				name:      "key",
				jsonInput: `"key"`,
				expected:  SSHAuthMethodKey,
			},
			{
				name:      "password",
				jsonInput: `"password"`,
				expected:  SSHAuthMethodPassword,
			},
			{
				name:      "unknown defaults to key",
				jsonInput: `"unknown"`,
				expected:  SSHAuthMethodKey,
			},
			{
				name:      "invalid json",
				jsonInput: `invalid_json`,
				expectErr: true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				var method SSHAuthMethod
				err := method.UnmarshalJSON([]byte(tc.jsonInput))
				if tc.expectErr {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, tc.expected, method)
			})
		}
	})

	t.Run("MarshalJSON", func(t *testing.T) {
		testCases := []struct {
			name     string
			input    SSHAuthMethod
			expected string
		}{
			{
				name:     "key",
				input:    SSHAuthMethodKey,
				expected: `"key"`,
			},
			{
				name:     "password",
				input:    SSHAuthMethodPassword,
				expected: `"password"`,
			},
			{
				name:     "default",
				input:    SSHAuthMethod(99),
				expected: `""`,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				data, err := tc.input.MarshalJSON()
				require.NoError(t, err)
				assert.JSONEq(t, tc.expected, string(data))
			})
		}
	})
}

func TestHostKeyPolicy_JSON(t *testing.T) {
	t.Run("UnmarshalJSON", func(t *testing.T) {
		testCases := []struct {
			name      string
			jsonInput string
			expected  HostKeyPolicy
			expectErr bool
		}{
			{
				name:      "ignore",
				jsonInput: `"ignore"`,
				expected:  IgnoreHostKey,
			},
			{
				name:      "strict",
				jsonInput: `"strict"`,
				expected:  RejectHostKeyIfMissing,
			},
			{
				name:      "accept-new",
				jsonInput: `"accept-new"`,
				expected:  AcceptNewHost,
			},
			{
				name:      "ask",
				jsonInput: `"ask"`,
				expected:  AskNewHost,
			},
			{
				name:      "unknown defaults to strict",
				jsonInput: `"unknown"`,
				expected:  RejectHostKeyIfMissing,
			},
			{
				name:      "invalid json",
				jsonInput: `invalid_json`,
				expectErr: true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				var policy HostKeyPolicy
				err := policy.UnmarshalJSON([]byte(tc.jsonInput))
				if tc.expectErr {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, tc.expected, policy)
			})
		}
	})

	t.Run("MarshalJSON", func(t *testing.T) {
		testCases := []struct {
			name     string
			input    HostKeyPolicy
			expected string
		}{
			{
				name:     "ignore",
				input:    IgnoreHostKey,
				expected: `"ignore"`,
			},
			{
				name:     "strict",
				input:    RejectHostKeyIfMissing,
				expected: `"strict"`,
			},
			{
				name:     "accept-new",
				input:    AcceptNewHost,
				expected: `"accept-new"`,
			},
			{
				name:     "ask",
				input:    AskNewHost,
				expected: `"ask"`,
			},
			{
				name:     "default",
				input:    HostKeyPolicy(99),
				expected: `""`,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				data, err := tc.input.MarshalJSON()
				require.NoError(t, err)
				assert.JSONEq(t, tc.expected, string(data))
			})
		}
	})
}
