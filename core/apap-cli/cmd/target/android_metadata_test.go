// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	targetloginmocks "github.com/Arm-Debug/apap-cli/apap-cli/service/targetlogin/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/api/mocks"
)

func ptr(s string) *string { return &s }

type noLocalhostVerifier struct{}

func (*noLocalhostVerifier) IsLocalhostSupported() bool { return false }

func TestAndroidAddressPersistence(t *testing.T) {
	t.Run("preserves target settings, default and rename", func(t *testing.T) {
		manager := engine_target.NewTargetManager(filepath.Join(t.TempDir(), "targets.json"), &noLocalhostVerifier{})
		original := &engine_target.AndroidTarget{SerialNumber: "phone", DeviceIPAddress: ptr("configured:5555"), LastKnownADBAddress: ptr("old:5555")}
		require.NoError(t, manager.AddTarget("my-phone", original))
		require.NoError(t, manager.SetDefaultTarget("my-phone"))
		client := &androidAddressClient{name: "my-phone", target: original, manager: manager}

		err := client.recordAddress(metadata.Pairs(grpcserver.ResolvedADBAddress, "new:5556"), nil)

		require.NoError(t, err)
		stored, err := manager.GetTarget("my-phone")
		require.NoError(t, err)
		assert.Equal(t, original, stored)
		assert.Equal(t, ptr("new:5556"), original.LastKnownADBAddress)
		assert.Equal(t, ptr("configured:5555"), original.DeviceIPAddress)
		name, err := manager.GetDefaultTargetName()
		require.NoError(t, err)
		assert.Equal(t, "my-phone", name)
		require.NoError(t, manager.UpdateTarget("my-phone", &engine_target.UpdateTargetFields{Name: "renamed", UpdatedTarget: stored}))
		stored, err = manager.GetTarget("renamed")
		require.NoError(t, err)
		assert.Equal(t, original, stored)
	})
	t.Run("absent metadata skips persistence and unchanged address is preserved", func(t *testing.T) {
		manager := &target.MockTargetManager{}
		original := &engine_target.AndroidTarget{SerialNumber: "phone", LastKnownADBAddress: ptr("old:5555")}
		client := &androidAddressClient{name: "my-phone", target: original, manager: manager}
		cause := errors.New("connection failed")
		manager.On("UpdateAndroidTargetLastKnownADBAddress", "my-phone", "phone", "old:5555").Return(true, nil).Once()

		missingErr := client.recordAddress(nil, cause)
		unchangedErr := client.recordAddress(metadata.Pairs(grpcserver.ResolvedADBAddress, "old:5555"), nil)

		require.ErrorIs(t, missingErr, cause)
		require.NoError(t, unchangedErr)
		assert.Equal(t, ptr("old:5555"), original.LastKnownADBAddress)
		manager.AssertNotCalled(t, "UpdateTarget", mock.Anything, mock.Anything)
		manager.AssertExpectations(t)
	})
	t.Run("write failure does not fail a successful operation", func(t *testing.T) {
		// Given a successful RPC and a target manager that cannot save metadata.
		manager := &target.MockTargetManager{}
		saveErr := errors.New("config is read only")
		manager.On("UpdateAndroidTargetLastKnownADBAddress", "my-phone", "phone", "new:5556").Return(false, saveErr).Once()
		original := &engine_target.AndroidTarget{SerialNumber: "phone", LastKnownADBAddress: ptr("old:5555")}
		client := &androidAddressClient{name: "my-phone", target: original, manager: manager}

		// When the verified address is recorded.
		err := client.recordAddress(metadata.Pairs(grpcserver.ResolvedADBAddress, "new:5556"), nil)

		// Then the operation remains successful and the in-memory target is unchanged.
		require.NoError(t, err)
		assert.Equal(t, ptr("old:5555"), original.LastKnownADBAddress)
		manager.AssertExpectations(t)
	})
	t.Run("write failure preserves the operation error", func(t *testing.T) {
		// Given a failed RPC and a target manager that cannot save metadata.
		manager := &target.MockTargetManager{}
		saveErr := errors.New("config is read only")
		callErr := errors.New("preparation failed")
		manager.On("UpdateAndroidTargetLastKnownADBAddress", "my-phone", "phone", "new:5556").Return(false, saveErr).Once()
		original := &engine_target.AndroidTarget{SerialNumber: "phone", LastKnownADBAddress: ptr("old:5555")}
		client := &androidAddressClient{name: "my-phone", target: original, manager: manager}

		// When the verified address is recorded.
		err := client.recordAddress(metadata.Pairs(grpcserver.ResolvedADBAddress, "new:5556"), callErr)

		// Then the RPC error remains the operation result and the target is unchanged.
		require.ErrorIs(t, err, callErr)
		assert.NotErrorIs(t, err, saveErr)
		assert.Equal(t, ptr("old:5555"), original.LastKnownADBAddress)
		manager.AssertExpectations(t)
	})
	t.Run("concurrent target edits are preserved", func(t *testing.T) {
		// Given a target captured before the RPC and an edit made while it is running.
		manager := engine_target.NewTargetManager(filepath.Join(t.TempDir(), "targets.json"), &noLocalhostVerifier{})
		original := &engine_target.AndroidTarget{SerialNumber: "phone", DeviceIPAddress: ptr("configured:5555"), LastKnownADBAddress: ptr("old:5555")}
		require.NoError(t, manager.AddTarget("my-phone", original))
		rpcTarget, err := manager.GetTarget("my-phone")
		require.NoError(t, err)
		client := &androidAddressClient{name: "my-phone", target: rpcTarget.(*engine_target.AndroidTarget), manager: manager}

		concurrentEdit, err := manager.GetTarget("my-phone")
		require.NoError(t, err)
		concurrentEdit.(*engine_target.AndroidTarget).DeviceIPAddress = ptr("edited:5555")
		require.NoError(t, manager.UpdateTarget("my-phone", &engine_target.UpdateTargetFields{UpdatedTarget: concurrentEdit}))

		// When the RPC completes with a newly verified address.
		err = client.recordAddress(metadata.Pairs(grpcserver.ResolvedADBAddress, "new:5556"), nil)

		// Then persistence updates the address without overwriting the concurrent edit.
		require.NoError(t, err)
		stored, err := manager.GetTarget("my-phone")
		require.NoError(t, err)
		android := stored.(*engine_target.AndroidTarget)
		assert.Equal(t, ptr("edited:5555"), android.DeviceIPAddress)
		assert.Equal(t, ptr("new:5556"), android.LastKnownADBAddress)
	})
	t.Run("target serial change skips stale address", func(t *testing.T) {
		// Given the configured target now has a different Android serial number.
		manager := engine_target.NewTargetManager(filepath.Join(t.TempDir(), "targets.json"), &noLocalhostVerifier{})
		original := &engine_target.AndroidTarget{SerialNumber: "phone", LastKnownADBAddress: ptr("old:5555")}
		require.NoError(t, manager.AddTarget("my-phone", original))
		client := &androidAddressClient{name: "my-phone", target: original, manager: manager}
		changedTarget := &engine_target.AndroidTarget{SerialNumber: "replacement-phone", LastKnownADBAddress: ptr("replacement:5555")}
		require.NoError(t, manager.UpdateTarget("my-phone", &engine_target.UpdateTargetFields{UpdatedTarget: changedTarget}))

		// When the in-flight RPC returns the previous target's address.
		err := client.recordAddress(metadata.Pairs(grpcserver.ResolvedADBAddress, "new:5556"), nil)

		// Then the replacement target is left untouched.
		require.NoError(t, err)
		stored, err := manager.GetTarget("my-phone")
		require.NoError(t, err)
		android := stored.(*engine_target.AndroidTarget)
		assert.Equal(t, "replacement-phone", android.SerialNumber)
		assert.Equal(t, ptr("replacement:5555"), android.LastKnownADBAddress)
	})
}

func TestAndroidChecksPersistEngineAddressAfterLaterFailures(t *testing.T) {
	previous := viper.GetBool(enableAndroidTargetsConfigKey)
	viper.Set(enableAndroidTargetsConfigKey, true)
	t.Cleanup(func() { viper.Set(enableAndroidTargetsConfigKey, previous) })
	for _, operation := range []string{"test", "prepare-check"} {
		t.Run(operation, func(t *testing.T) {
			manager := engine_target.NewTargetManager(filepath.Join(t.TempDir(), "targets.json"), &noLocalhostVerifier{})
			original := &engine_target.AndroidTarget{SerialNumber: "phone", LastKnownADBAddress: ptr("old:5555")}
			require.NoError(t, manager.AddTarget("my-phone", original))
			require.NoError(t, manager.SetDefaultTarget("my-phone"))
			client := apapprotomocks.NewApapClient(t)
			connector := &mocks.MockAutostartClientConnector{}
			connector.SetClient(client, nil)
			login := targetloginmocks.NewMockLoginService(t)
			cause := errors.New("later check failure")
			setAddress := func(args mock.Arguments) {
				option := args.Get(2).(grpc.TrailerCallOption)
				*option.TrailerAddr = metadata.Pairs(grpcserver.ResolvedADBAddress, "engine:5556")
			}
			var err error
			if operation == "test" {
				client.On("TargetTest", mock.Anything, mock.MatchedBy(func(request *apapproto.TargetTestRequest) bool {
					return request.Target.GetAndroidConfig().SerialNumber == "phone"
				}), mock.Anything).Run(setAddress).Return((*apapproto.TargetTestResponse)(nil), cause).Once()
				cmd := &cobra.Command{}
				cmd.SetContext(context.Background())

				err = targetTestCommand(cmd, &bytes.Buffer{}, []string{"my-phone"}, connector, manager, &target.ConcreteTargetTester{}, login, "")
			} else {
				previous := check
				check = true
				t.Cleanup(func() { check = previous })
				client.On("TargetPrepare", mock.Anything, mock.MatchedBy(func(request *apapproto.TargetPrepareRequest) bool {
					return request.DeploymentType == apapproto.ToolDeploy_CHECK && request.Target.GetAndroidConfig().SerialNumber == "phone"
				}), mock.Anything).Run(setAddress).Return((*apapproto.TargetPrepareResponse)(nil), cause).Once()
				err = prepareTarget(connector, manager, login, &bytes.Buffer{}, "")
			}
			require.ErrorIs(t, err, cause)
			stored, loadErr := manager.GetTarget("my-phone")
			require.NoError(t, loadErr)
			assert.Equal(t, ptr("engine:5556"), stored.(*engine_target.AndroidTarget).LastKnownADBAddress)
			login.AssertNotCalled(t, "LoginToTarget", mock.Anything, mock.Anything, mock.Anything)
		})
	}
}
