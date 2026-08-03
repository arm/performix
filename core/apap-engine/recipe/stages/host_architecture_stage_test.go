// Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
//
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"errors"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	conductormocks "github.com/Arm-Debug/apap-cli/apap-engine/conductor/conductormocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	targetsessionmocks "github.com/Arm-Debug/apap-cli/apap-engine/targetsession/mocks"
)

func TestHostArchitectureStage(t *testing.T) {
	hostBundlesSupplier := func() []deploymentsupport.ToolBundleInfo {
		return []deploymentsupport.ToolBundleInfo{{Locality: deploymentsupport.DeploymentLocalityHost}}
	}

	t.Run("Execute skips when no host bundles are resolved", func(t *testing.T) {
		provider := &targetsessionmocks.MockTargetSessionProvider{}
		stage := NewHostArchitectureStage(provider, func() []deploymentsupport.ToolBundleInfo {
			return []deploymentsupport.ToolBundleInfo{{Locality: deploymentsupport.DeploymentLocalityTarget}}
		})

		closer, err := stage.Execute(&recipe.StageContext{})
		require.NoError(t, err)
		require.Nil(t, closer)
		require.Nil(t, stage.TargetSessionSupplier())
		require.Nil(t, stage.TargetFilesystemSupplier())
		require.Nil(t, stage.CommandRunnerSupplier())
		require.Equal(t, conductor.PlatformConfiguration{}, stage.PlatformConfigurationSupplier())
		provider.AssertNotCalled(t, "TargetSession", mock.Anything)
	})

	t.Run("Execute success", func(t *testing.T) {
		platform := conductor.PlatformConfiguration{OS: conductor.Darwin, Architecture: conductor.AArch64}
		targetFilesystem := conductor.NewAferoTargetFilesystem(afero.NewOsFs())
		session := &targetsessionmocks.MockTargetSession{}
		connection := &targetsessionmocks.MockTargetConnection{}
		hostPlatform := &conductor.TargetPlatform{PlatformConfiguration: platform}
		commandRunner := &conductormocks.MockCommandRunner{}
		connection.On("CommandRunner").Return(commandRunner).Once()
		connection.On("Filesystem").Return(targetFilesystem).Once()
		session.On("Connect", mock.Anything, targetsession.ConnectOptions{PlatformGate: conductor.HostSupported}).Return(connection, nil).Once()
		session.On("TargetPlatform").Return(hostPlatform, nil).Once()

		provider := &targetsessionmocks.MockTargetSessionProvider{}
		provider.On("HostSession").Return(session, nil).Once()

		stage := NewHostArchitectureStage(provider, hostBundlesSupplier)
		require.Equal(t, "Identifying host architecture", stage.Name())

		closer, err := stage.Execute(&recipe.StageContext{})
		require.NoError(t, err)
		require.Nil(t, closer)
		require.Equal(t, platform, stage.PlatformConfiguration)
		require.Same(t, session, stage.TargetSessionSupplier())
		require.Same(t, targetFilesystem, stage.TargetFilesystemSupplier())
		require.Same(t, commandRunner, stage.CommandRunnerSupplier())
		provider.AssertExpectations(t)
		session.AssertExpectations(t)
		connection.AssertExpectations(t)
	})

	t.Run("Execute failure when session provider fails", func(t *testing.T) {
		provider := &targetsessionmocks.MockTargetSessionProvider{}
		sessionErr := errors.New("cant get session")
		provider.On("HostSession").Return((*targetsessionmocks.MockTargetSession)(nil), sessionErr).Once()

		stage := NewHostArchitectureStage(provider, hostBundlesSupplier)

		_, err := stage.Execute(&recipe.StageContext{})
		require.ErrorIs(t, err, sessionErr)
		provider.AssertExpectations(t)
	})

	t.Run("Execute failure when host platform cannot be retrieved", func(t *testing.T) {
		session := &targetsessionmocks.MockTargetSession{}
		connection := &targetsessionmocks.MockTargetConnection{}
		session.On("Connect", mock.Anything, targetsession.ConnectOptions{PlatformGate: conductor.HostSupported}).Return(connection, nil).Once()
		session.On("TargetPlatform").Return((*conductor.TargetPlatform)(nil), errors.New("platform unavailable")).Once()

		provider := &targetsessionmocks.MockTargetSessionProvider{}
		provider.On("HostSession").Return(session, nil).Once()

		stage := NewHostArchitectureStage(provider, hostBundlesSupplier)
		_, err := stage.Execute(&recipe.StageContext{})
		require.Error(t, err)
		provider.AssertExpectations(t)
		session.AssertExpectations(t)
	})
}
