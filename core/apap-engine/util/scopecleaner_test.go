// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCloseRenderSession(t *testing.T) {
	t.Run("calls deferred action on all resources on error return", func(t *testing.T) {
		cleanup1Done := false
		cleanup2Done := false

		f := func() {
			sc := ScopeCleaner{}
			defer sc.MaybeCleanup(func() { cleanup1Done = true })
			defer sc.MaybeCleanup(func() { cleanup2Done = true })
		}
		f()

		assert.Equal(t, true, cleanup1Done)
		assert.Equal(t, true, cleanup2Done)
	})

	t.Run("does nothing on success return", func(t *testing.T) {
		cleanup1Done := false
		cleanup2Done := false

		{
			sc := ScopeCleaner{}
			defer sc.MaybeCleanup(func() { cleanup1Done = true })
			defer sc.MaybeCleanup(func() { cleanup2Done = true })
			sc.CancelCleanup()
		}

		assert.Equal(t, false, cleanup1Done)
		assert.Equal(t, false, cleanup2Done)
	})
}
