// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	targetsessionmocks "github.com/Arm-Debug/apap-cli/apap-engine/targetsession/mocks"
)

func TestTargetArchitectureStage(t *testing.T) {
	t.Run("Execute success", func(t *testing.T) {
		platform := &conductor.TargetPlatform{PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Linux}}
		session := &targetsessionmocks.MockTargetSession{}
		session.On("TargetPlatform").Return(platform, nil).Once()

		stage := NewTargetArchitectureStage(func() targetsession.TargetSession { return session })

		closer, err := stage.Execute(&recipe.StageContext{})
		require.NoError(t, err)
		require.Nil(t, closer)
		require.Same(t, platform, stage.TargetPlatform)
		require.Equal(t, platform.PlatformConfiguration, stage.PlatformConfiguration)
		session.AssertExpectations(t)
	})

	t.Run("Execute failure", func(t *testing.T) {
		session := &targetsessionmocks.MockTargetSession{}
		tpErr := errors.New("cant get platform")
		session.On("TargetPlatform").Return((*conductor.TargetPlatform)(nil), tpErr).Once()

		stage := NewTargetArchitectureStage(func() targetsession.TargetSession { return session })

		_, err := stage.Execute(&recipe.StageContext{})
		require.ErrorIs(t, err, tpErr)
		session.AssertExpectations(t)
	})
}
