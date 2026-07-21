// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package ssh

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func TestParseSSHLogin(t *testing.T) {
	tests := []struct {
		name              string
		input             string
		want              LoginString
		expectError       bool
		expectedErrorCode message.MessageCode
		expectedMetadata  map[string]string
	}{
		{
			name:  "host only",
			input: "example.com",
			want: LoginString{
				User: "",
				Host: "example.com",
				Port: 0,
			},
		},
		{
			name:  "user and host",
			input: "alice@example.com",
			want: LoginString{
				User:               "alice",
				Host:               "example.com",
				Port:               0,
				PrivateKeyFilename: "",
			},
		},
		{
			name:  "host and port",
			input: "example.com:2200",
			want: LoginString{
				User:               "",
				Host:               "example.com",
				Port:               2200,
				PrivateKeyFilename: "",
			},
		},
		{
			name:  "host and key",
			input: "example.com:my_private_key_path",
			want: LoginString{
				User:               "",
				Host:               "example.com",
				Port:               0,
				PrivateKeyFilename: "my_private_key_path",
			},
		},
		{
			name:  "user, host, port and key",
			input: "bob@host.local:2022:my_private_key_path",
			want: LoginString{
				User:               "bob",
				Host:               "host.local",
				Port:               2022,
				PrivateKeyFilename: "my_private_key_path",
			},
		},
		{
			name:  "host with auth",
			input: "example.com:auth=password",
			want: LoginString{
				User:               "",
				Host:               "example.com",
				Port:               0,
				PrivateKeyFilename: "",
				AuthMethod:         "password",
			},
		},
		{
			name:  "host port with auth",
			input: "example.com:2222:auth=password",
			want: LoginString{
				User:               "",
				Host:               "example.com",
				Port:               2222,
				PrivateKeyFilename: "",
				AuthMethod:         "password",
			},
		},
		{
			name:  "user host key and auth",
			input: "alice@example.com:/path/to/key:auth=key",
			want: LoginString{
				User:               "alice",
				Host:               "example.com",
				Port:               0,
				PrivateKeyFilename: "/path/to/key",
				AuthMethod:         "key",
			},
		},
		{
			name:  "key with colon in path and no auth",
			input: "alice@host.com:C:\\Users\\Alice\\.ssh\\id_rsa",
			want: LoginString{
				User:               "alice",
				Host:               "host.com",
				Port:               0,
				PrivateKeyFilename: "C:\\Users\\Alice\\.ssh\\id_rsa",
			},
		},
		{
			name:  "key with colon in path and auth",
			input: "alice@host.com:C:\\Users\\Alice\\.ssh\\id_rsa:auth=key",
			want: LoginString{
				User:               "alice",
				Host:               "host.com",
				Port:               0,
				PrivateKeyFilename: "C:\\Users\\Alice\\.ssh\\id_rsa",
				AuthMethod:         "key",
			},
		},
		{
			name:              "empty string",
			input:             "",
			expectError:       true,
			expectedErrorCode: message.EngineSshMissingHost,
			expectedMetadata: map[string]string{
				"loginString": "",
			},
		},
		{
			name:              "only user",
			input:             "root@",
			expectError:       true,
			expectedErrorCode: message.EngineSshMissingHost,
			expectedMetadata: map[string]string{
				"loginString": "root@",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLoginString(tt.input)
			if tt.expectError {
				var msgErr message.Message
				ok := errors.As(err, &msgErr)
				assert.True(t, ok)
				assert.Equal(t, tt.expectedErrorCode, msgErr.Code())
				for key, expectedValue := range tt.expectedMetadata {
					assert.Equal(t, expectedValue, msgErr.Metadata()[key])
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
