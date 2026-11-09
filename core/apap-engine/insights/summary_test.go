// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func TestNewRunSummary(t *testing.T) {
	t.Run("returns catalog error when payload cannot be marshalled", func(t *testing.T) {
		_, err := NewRunSummary("bad_payload", "bad payload", map[string]any{"value": func() {}})

		require.Error(t, err)
		msg, ok := err.(*message.MessageImpl)
		require.True(t, ok)
		assert.Equal(t, message.EngineInsightsRunSummaryMarshalFailed, msg.Code())
		require.Error(t, msg.Unwrap())
		assert.Contains(t, msg.Unwrap().Error(), "unsupported type")
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}
