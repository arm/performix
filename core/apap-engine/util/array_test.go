// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInitialise(t *testing.T) {
	t.Run("fills slice with default int", func(t *testing.T) {
		got := Initialise(3, 7)
		assert.Equal(t, []int{7, 7, 7}, got)
	})

	t.Run("returns empty but non-nil slice for zero length", func(t *testing.T) {
		got := Initialise(0, "value")
		assert.NotNil(t, got)
		assert.Equal(t, []string{}, got)
	})

	t.Run("uses explicitly-provided type", func(t *testing.T) {
		got := Initialise[any](3, "value")
		assert.Equal(t, []any{"value", "value", "value"}, got)
	})

	t.Run("copies value types into each element", func(t *testing.T) {
		type counter struct {
			n int
		}
		defaultValue := counter{n: 1}

		got := Initialise(2, defaultValue)
		got[0].n = 5

		assert.Equal(t, []counter{{n: 5}, {n: 1}}, got)
	})
}
