// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMakeVisualizationParameterKey(t *testing.T) {
	t.Run("successfully constructs visualization parameter key", func(t *testing.T) {
		got := MakeVisualizationParameterKey("foo", "bar")
		assert.Equal(t, "foo.bar", got)
	})
}

func TestSplitVisualizationParameterKey(t *testing.T) {
	t.Run("successfully deconstructs visualization parameter key", func(t *testing.T) {
		vizID, paramID, ok := SplitVisualizationParameterKey("foo.bar")
		assert.True(t, ok)
		assert.Equal(t, vizID, "foo")
		assert.Equal(t, paramID, "bar")
	})

	t.Run("cannot deconstruct empty visualization parameter key", func(t *testing.T) {
		_, _, ok := SplitVisualizationParameterKey("")
		assert.False(t, ok)
	})

	t.Run("cannot deconstruct dot-less visualization parameter key", func(t *testing.T) {
		_, _, ok := SplitVisualizationParameterKey("foo")
		assert.False(t, ok)
	})

	t.Run("visualization parameter key cannot have empty visualization id", func(t *testing.T) {
		_, _, ok := SplitVisualizationParameterKey(".bar")
		assert.False(t, ok)
	})

	t.Run("visualization parameter key cannot have empty parameter id", func(t *testing.T) {
		_, _, ok := SplitVisualizationParameterKey("foo.")
		assert.False(t, ok)
	})
}
