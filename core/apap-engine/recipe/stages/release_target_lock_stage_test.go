// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func TestReleaseTargetLockStage(t *testing.T) {
	t.Run("releases target lock", func(t *testing.T) {
		released := false
		stage := NewReleaseTargetLockStage(func() {
			released = true
		})

		cleanup, err := stage.Execute(&recipe.StageContext{})

		require.NoError(t, err)
		require.Nil(t, cleanup)
		require.True(t, released)
		require.Equal(t, "Releasing target lock", stage.Name())
		require.Equal(t, run.RecipeFailureTargetLock, stage.ErrorType())
		require.True(t, stage.AlwaysExecute())
	})

	t.Run("allows missing release callback", func(t *testing.T) {
		stage := NewReleaseTargetLockStage(nil)

		cleanup, err := stage.Execute(&recipe.StageContext{})

		require.NoError(t, err)
		require.Nil(t, cleanup)
	})
}
