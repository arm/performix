// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package terminology

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func restoreRawNames(t *testing.T) {
	originalTerms := rawTerms
	t.Cleanup(func() {
		rawTerms = originalTerms
		setTerms()
	})
}

func TestSetNames(t *testing.T) {
	t.Run("stores terms correctly", func(t *testing.T) {
		restoreRawNames(t)
		rawTerms = []byte(`{
			"PRODUCT_FULL_NAME": "LengthyName",
			"PRODUCT_BINARY_NAME": "bin-name",
			"AGENT_BINARY_NAME": "agent-bin-name",
			"DAEMON_DIR_NAME": "daemon-dir",
			"ENV_VAR_PREFIX": "ENV_VAR"
		}`)
		setTerms()

		assert.Equal(t, "LengthyName", GetProductFullName())
		assert.Equal(t, "bin-name", GetProductBinaryName())
		assert.Equal(t, "agent-bin-name", GetAgentBinaryName())
		assert.Equal(t, "daemon-dir", GetDaemonDirName())
		assert.Equal(t, "ENV_VAR", GetEnvVarPrefix())
	})
}

func TestTerminologyJSON(t *testing.T) {
	t.Run("all fields are defined", func(t *testing.T) {
		setTerms()
		assert.NotEmpty(t, GetProductFullName(), "PRODUCT_FULL_NAME is not defined in terminology.json")
		assert.NotEmpty(t, GetProductBinaryName(), "PRODUCT_BINARY_NAME is not defined in terminology.json")
		assert.NotEmpty(t, GetAgentBinaryName(), "AGENT_BINARY_NAME is not defined in terminology.json")
		assert.NotEmpty(t, GetDaemonDirName(), "DAEMON_DIR_NAME is not defined in terminology.json")
		assert.NotEmpty(t, GetEnvVarPrefix(), "ENV_VAR_PREFIX is not defined in terminology.json")
	})
}
