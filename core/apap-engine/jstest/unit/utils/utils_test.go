// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	tool_goja "github.com/Arm-Debug/apap-cli/apap-engine/tool/goja"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

// This file contains a few demonstrative JS unit tests

func TestDecodeXMLAttribute(t *testing.T) {
	h := jstest.LoadJSModule(t, "tool-integrations/utils.js")

	tests := []struct {
		name    string
		encoded string
		decoded string
	}{
		{name: "plain text", encoded: "JVMInformation", decoded: "JVMInformation"},
		{name: "predefined entities", encoded: "&amp;&apos;&gt;&lt;&quot;", decoded: `&'><"`},
		{name: "decimal entities", encoded: "&#9;&#10;&#13;&#32;&#57344;&#65536;", decoded: "\t\n\r \ue000\U00010000"},
		{name: "hexadecimal entities", encoded: "&#x2e;&#X4A;", decoded: ".J"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := h.Call[string](t, "decodeXmlAttribute", test.encoded)

			require.NoError(t, err)
			require.Equal(t, test.decoded, result)
		})
	}

	t.Run("rejects an unknown entity", func(t *testing.T) {
		_, err := h.Call[string](t, "decodeXmlAttribute", "jdk&unknown;Event")

		require.ErrorContains(t, err, "Invalid XML entity '&unknown;'")
	})

	t.Run("rejects an invalid XML code point", func(t *testing.T) {
		_, err := h.Call[string](t, "decodeXmlAttribute", "jdk&#0;Event")

		require.ErrorContains(t, err, "Invalid XML entity '&#0;'")
	})

	t.Run("rejects an unterminated entity", func(t *testing.T) {
		_, err := h.Call[string](t, "decodeXmlAttribute", "jdk&amp")

		require.ErrorContains(t, err, "Malformed XML entity in attribute value")
	})
}

func TestEnsureDeployed(t *testing.T) {
	t.Run("errors if path does not exist", func(t *testing.T) {
		h := jstest.LoadJSModule(t, "tool-integrations/utils.js")
		deployPath := "myDeployPath"
		toolName := "myToolName"
		locality := "host"

		engine := mocks.MockToolEngine{}
		engine.On("GetPlatform").Return(conductor.PlatformConfiguration{OS: "Linux"}, nil)
		engine.On("ExecCommand", []string{"stat", deployPath}, mock.Anything).
			Return(h.ToJSValPromise(t, process.CommandResult{Rc: 1}, nil))
		engine.On("GetLocality").Return(locality, nil)

		result, err := h.CallAwait[any](t, "ensureDeployed", &engine, deployPath, toolName)
		assert.Nil(t, result)

		expectedMetadata := map[string]string{
			"tool":       toolName,
			"deployPath": deployPath,
			"locality":   locality,
		}
		expectedErr := message.New(message.ToolIntegrationsCommonToolNotDeployed).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
	})
}

func TestIsElevatePrivilegeError(t *testing.T) {
	testCases := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "non-privilege error",
			err:      errors.New("this is a test"),
			expected: false,
		},
		{
			name:     "privilege error",
			err:      errors.New(message.EngineToolServiceElevatePrivilegesFailed),
			expected: true,
		},
	}
	h := jstest.LoadJSModule(t, "tool-integrations/utils.js")
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := h.Call[bool](t, "isElevatePrivilegeError", tc.err)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestResolveLoginName(t *testing.T) {
	h := jstest.LoadJSModule(t, "tool-integrations/utils.js")
	t.Run("uses logname where available", func(t *testing.T) {
		name := "  abc123  "
		mockEngine := mocks.MockToolEngine{}
		mockEngine.On("ExecCommand", []string{"logname"}, tool_goja.ExecOptions{}).
			Return(h.ToJSValPromise(t, process.CommandResult{Rc: 0, Stdout: name}, nil))

		result, err := h.CallAwait[string](t, "resolveLoginName", &mockEngine)
		require.NoError(t, err)
		assert.Equal(t, "abc123", result)
	})
	t.Run("falls back to SUDO_USER", func(t *testing.T) {
		env := `ABC=123
USERNAME=password
USER=123
SUDO_USER=abc
`
		mockEngine := mocks.MockToolEngine{}
		mockEngine.On("ExecCommand", []string{"env"}, tool_goja.ExecOptions{}).
			Return(h.ToJSValPromise(t, process.CommandResult{Rc: 0, Stdout: env}, nil))
		mockEngine.On("ExecCommand", []string{"logname"}, tool_goja.ExecOptions{}).
			Return(h.ToJSValPromise(t, process.CommandResult{Rc: 1}, nil))

		result, err := h.CallAwait[string](t, "resolveLoginName", &mockEngine)
		require.NoError(t, err)
		assert.Equal(t, result, "abc")
	})
	t.Run("falls back to USER", func(t *testing.T) {
		env := `ABC=123
USERNAME=password
USER=123
`
		mockEngine := mocks.MockToolEngine{}
		mockEngine.On("ExecCommand", []string{"env"}, tool_goja.ExecOptions{}).
			Return(h.ToJSValPromise(t, process.CommandResult{Rc: 0, Stdout: env}, nil))
		mockEngine.On("ExecCommand", []string{"logname"}, tool_goja.ExecOptions{}).
			Return(h.ToJSValPromise(t, process.CommandResult{Rc: 1}, nil))

		result, err := h.CallAwait[string](t, "resolveLoginName", &mockEngine)
		require.NoError(t, err)
		assert.Equal(t, result, "123")
	})
	t.Run("errors if no login name can be found", func(t *testing.T) {
		env := `ABC=123
USERNAME=password
`
		mockEngine := mocks.MockToolEngine{}
		mockEngine.On("ExecCommand", []string{"env"}, tool_goja.ExecOptions{}).
			Return(h.ToJSValPromise(t, process.CommandResult{Rc: 0, Stdout: env}, nil))
		mockEngine.On("ExecCommand", []string{"logname"}, tool_goja.ExecOptions{}).
			Return(h.ToJSValPromise(t, process.CommandResult{Rc: 1}, nil))

		result, err := h.CallAwait[string](t, "resolveLoginName", &mockEngine)
		assert.Equal(t, "", result)

		expectedMetadata := map[string]string{
			"lognameRc": "1",
			"envRc":     "0",
		}
		expectedErr := message.New(message.ToolIntegrationsCommonLoginNameNotFound).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}
