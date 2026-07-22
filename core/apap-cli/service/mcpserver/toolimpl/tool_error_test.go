// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func TestNewToolError(t *testing.T) {
	t.Run("keeps a plain local error message", func(t *testing.T) {
		te := newToolError(errors.New("run_id is required"))

		require.NotNil(t, te)
		assert.Equal(t, "run_id is required", te.Message)
		assert.Equal(t, string(message.SeverityError), te.Severity)
		assert.Empty(t, te.Code)
		assert.Empty(t, te.Explanation)
		assert.Empty(t, te.Advice)
	})

	t.Run("surfaces catalog detail from a message error", func(t *testing.T) {
		te := newToolError(message.New(message.EngineRunDoesNotExist))

		require.NotNil(t, te)
		assert.Equal(t, message.EngineRunDoesNotExist, te.Code)
		assert.NotEmpty(t, te.Severity)
		assert.NotEmpty(t, te.Message)
		assert.NotEmpty(t, te.Explanation)
		assert.NotEmpty(t, te.Advice)
	})

	t.Run("preserves child error detail", func(t *testing.T) {
		te := newToolError(message.New(message.EngineRunDoesNotExist).WithCause(errors.New("inner error")))

		require.NotNil(t, te)
		require.Len(t, te.Children, 1)
		assert.Equal(t, "inner error", te.Children[0].Message)
		assert.Equal(t, string(message.SeverityError), te.Children[0].Severity)
	})
}
