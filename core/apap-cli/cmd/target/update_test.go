// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestInvalidParameters(t *testing.T) {
	mtm := target.MockTargetManager{}
	client := apapprotomocks.NewApapClient(t)
	cc := &mocks.MockAutostartClientConnector{}
	cc.SetClient(client, nil)
	cmd := newUpdateCommand(cc, &mtm)
	t.Run("test invalid name parameter fails", func(t *testing.T) {
		cmd.SetArgs([]string{"test", "--name"})
		_, err := cmd.ExecuteC()
		assert.Error(t, err)
	})
	t.Run("test invalid host parameter fails", func(t *testing.T) {
		cmd.SetArgs([]string{"test", "--host"})
		_, err := cmd.ExecuteC()
		assert.Error(t, err)
	})
	t.Run("test invalid jump parameter fails", func(t *testing.T) {
		cmd.SetArgs([]string{"test", "--jump"})
		_, err := cmd.ExecuteC()
		assert.Error(t, err)
	})
	t.Run("test empty parameter fails", func(t *testing.T) {
		cmd.SetArgs([]string{})
		_, err := cmd.ExecuteC()
		assert.ErrorContains(t, err, "accepts 1 arg(s), received 0")
	})
	t.Run("test not specifying any update field fails", func(t *testing.T) {
		cmd.SetArgs([]string{"test"})
		_, err := cmd.ExecuteC()
		expectedErr := message.New(message.CliCmdTargetUpdateMissingProperty)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}

func TestUpdateCommand(t *testing.T) {
	t.Run("no output on success without --json", func(t *testing.T) {
		mtm := &target.MockTargetManager{}
		cc := &mocks.MockAutostartClientConnector{}

		tgtToUpdate := engine_target.SSHTarget{}
		mtm.On("GetTarget", testTargetName).Return(&tgtToUpdate, nil)

		updatedTargetFields := engine_target.UpdateTargetFields{UpdatedTarget: &tgtToUpdate, Name: "newName"}
		mtm.On("UpdateTarget", testTargetName, &updatedTargetFields).Return(nil)

		buf := &bytes.Buffer{}
		cmd := newUpdateCommand(cc, mtm)
		cmd.SetOut(buf)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{testTargetName, "--name", "newName"})
		err := cmd.Execute()
		assert.NoError(t, err)

		assert.Empty(t, buf.String())
		mtm.AssertExpectations(t)
	})
	t.Run("output on success with --json", func(t *testing.T) {
		mtm := &target.MockTargetManager{}
		cc := &mocks.MockAutostartClientConnector{}

		tgtToUpdate := engine_target.SSHTarget{}
		mtm.On("GetTarget", testTargetName).Return(&tgtToUpdate, nil)

		updatedTargetFields := engine_target.UpdateTargetFields{UpdatedTarget: &tgtToUpdate, Name: "newName"}
		mtm.On("UpdateTarget", testTargetName, &updatedTargetFields).Return(nil)

		cmdBuf := &bytes.Buffer{}
		cmd := newUpdateCommand(cc, mtm)
		cmd.SetOut(cmdBuf)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{testTargetName, "--name", "newName", "--json"})
		err := cmd.Execute()
		assert.NoError(t, err)

		assert.True(t, utils.IsValidJSON(cmdBuf.String()))
		assert.Contains(t, cmdBuf.String(), `"code":"0"`)
		assert.Contains(t, cmdBuf.String(), `"data":{}`)
		assert.Contains(t, cmdBuf.String(), `"error":{"message_code":""`)
		mtm.AssertExpectations(t)
	})
}

func TestUpdateCommandHostKeyPolicyValues(t *testing.T) {
	invalidValues := []string{"", "foo", "true", "false", "yess", "none"}

	for _, value := range invalidValues {
		t.Run("invalid-"+value, func(t *testing.T) {
			cmd := newUpdateCommand(nil, &target.MockTargetManager{})
			cmd.SetArgs([]string{testTargetName, "--host-key-policy", value})

			err := cmd.Execute()
			expectedMetadata := map[string]string{
				"value": value,
				"flag":  "--host-key-policy",
			}
			expectedErr := message.New(message.CliCmdValidationInvalidFlagValue).WithMetadata(expectedMetadata)
			assert.Equal(t, expectedErr, err)
			assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		})
	}
}

func TestUpdateCommandFindKeys(t *testing.T) {

	t.Run("target update with --find-keys fails when connector fails", func(t *testing.T) {
		expectedError := errors.New("rekt")
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(nil, expectedError)

		mtm := target.MockTargetManager{}
		tgtToUpdate := engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{{}}}
		mtm.On("GetTarget", testTargetName).Return(&tgtToUpdate, nil)
		cmd := newUpdateCommand(cc, &mtm)
		cmd.SetArgs([]string{testTargetName, "--find-keys"})

		err := cmd.Execute()
		assert.ErrorContains(t, err, "rekt")
	})

	t.Run("target update is successful when a key is found using --find-keys", func(t *testing.T) {
		mtm := &target.MockTargetManager{}
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		client.On("FindSSHKeysForTarget", mock.Anything, mock.Anything).Return(
			&apapproto.SSHKeyResponse{PrivateKeyPaths: &apapproto.StringArray{Values: []string{"/thePath"}}}, nil)

		tgtToUpdate := engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{{}}}
		mtm.On("GetTarget", testTargetName).Return(&tgtToUpdate, nil)
		mtm.On("UpdateTarget", testTargetName, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
			tgt := args.Get(1).(*engine_target.UpdateTargetFields)
			expectedTarget := engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{{PrivateKeyFilename: "/thePath"}}}
			updatedTargetFields := engine_target.UpdateTargetFields{UpdatedTarget: &expectedTarget}
			assert.True(t, reflect.DeepEqual(&tgt.UpdatedTarget, &updatedTargetFields.UpdatedTarget))
		})
		cmd := newUpdateCommand(cc, mtm)
		cmd.SetArgs([]string{testTargetName, "--find-keys"})
		err := cmd.Execute()
		assert.NoError(t, err)
		mtm.AssertExpectations(t)
		client.AssertExpectations(t)
	})

	t.Run("target update fails when no key is found using --find-keys", func(t *testing.T) {
		mtm := &target.MockTargetManager{}
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		client.On("FindSSHKeysForTarget", mock.Anything, mock.Anything).Return(
			&apapproto.SSHKeyResponse{Error: message.BuildErrorChain(errors.New("failed to find ssh key"))},
			nil,
		)

		tgtToUpdate := engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{{}}}
		mtm.On("GetTarget", testTargetName).Return(&tgtToUpdate, nil)
		cmd := newUpdateCommand(cc, mtm)
		cmd.SetArgs([]string{testTargetName, "--find-keys"})
		err := cmd.Execute()
		assert.ErrorContains(t, err, "failed to find ssh key")
		mtm.AssertExpectations(t)
		client.AssertExpectations(t)
	})
}

func TestUpdateCommandAuthMethods(t *testing.T) {
	t.Run("auth methods apply to jumps and target", func(t *testing.T) {
		mtm := &target.MockTargetManager{}
		cc := &mocks.MockAutostartClientConnector{}

		tgtToUpdate := engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{{}, {}}}
		mtm.On("GetTarget", testTargetName).Return(&tgtToUpdate, nil)
		mtm.On("UpdateTarget", testTargetName, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
			fields := args.Get(1).(*engine_target.UpdateTargetFields)
			updated := fields.UpdatedTarget.(*engine_target.SSHTarget)
			assert.Len(t, updated.Jumps, 2)
			assert.Equal(t, engine_target.SSHAuthMethodPassword, updated.Jumps[0].AuthMethod)
			assert.Equal(t, engine_target.SSHAuthMethodKey, updated.Jumps[1].AuthMethod)
		})

		cmd := newUpdateCommand(cc, mtm)
		cmd.SetArgs([]string{
			testTargetName,
			"--jump", "jumpuser@222.222.222.222:auth=password",
			"--host", testTargetUser + "@111.111.111.111:auth=key",
		})

		err := cmd.Execute()
		assert.NoError(t, err)
	})

	t.Run("invalid auth method", func(t *testing.T) {
		mtm := &target.MockTargetManager{}
		cc := &mocks.MockAutostartClientConnector{}

		tgtToUpdate := engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{{}}}
		mtm.On("GetTarget", testTargetName).Return(&tgtToUpdate, nil)

		cmd := newUpdateCommand(cc, mtm)
		cmd.SetArgs([]string{
			testTargetName,
			"--host", testTargetUser + "@111.111.111.111:auth=bad_method",
		})

		err := cmd.Execute()

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CliCmdValidationInvalidAuthMethod, msgErr.Code())
		assert.Equal(t, "bad_method", msgErr.Metadata()["value"])
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("password auth and --find-keys are mutually exclusive", func(t *testing.T) {
		mtm := &target.MockTargetManager{}
		cc := &mocks.MockAutostartClientConnector{}

		tgtToUpdate := engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{{}, {}}}
		mtm.On("GetTarget", testTargetName).Return(&tgtToUpdate, nil)

		cmd := newUpdateCommand(cc, mtm)
		cmd.SetArgs([]string{
			testTargetName,
			"--host", testTargetUser + "@111.111.111.111:auth=password",
			"--find-keys",
		})

		err := cmd.Execute()

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CliCmdValidationPasswordAuthUnsupportedFlag, msgErr.Code())
		assert.Equal(t, "--find-keys", msgErr.Metadata()["flag"])
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("password auth rejected with key", func(t *testing.T) {
		mtm := &target.MockTargetManager{}
		cc := &mocks.MockAutostartClientConnector{}

		tgtToUpdate := engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{{
			PrivateKeyFilename: "/path/to/key",
		}}}
		mtm.On("GetTarget", testTargetName).Return(&tgtToUpdate, nil)

		cmd := newUpdateCommand(cc, mtm)
		cmd.SetArgs([]string{
			testTargetName,
			"--host", testTargetUser + "@111.111.111.111:/path/to/key:auth=password",
		})

		err := cmd.Execute()

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CliCmdValidationPasswordAuthWithKey, msgErr.Code())
		assert.Equal(t, "111.111.111.111", msgErr.Metadata()["host"])
		assert.Equal(t, "/path/to/key", msgErr.Metadata()["key"])
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}

func TestUpdateJSONStringTarget(t *testing.T) {
	t.Run("update with valid JSON target succeeds", func(t *testing.T) {
		mtm := &target.MockTargetManager{}
		cc := &mocks.MockAutostartClientConnector{}

		tgtToUpdate := engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{{}}}
		mtm.On("GetTarget", testTargetName).Return(&tgtToUpdate, nil)

		expectedTarget := GetTestJSONTarget()
		updatedTargetFields := engine_target.UpdateTargetFields{UpdatedTarget: &expectedTarget}
		mtm.On("UpdateTarget", testTargetName, &updatedTargetFields).Return(nil)

		cmd := newUpdateCommand(cc, mtm)
		cmd.SetArgs([]string{testTargetName, "--host", testJSONTargetString})
		err := cmd.Execute()
		assert.NoError(t, err)
		mtm.AssertExpectations(t)
	})

	t.Run("update with JSON target and --find-keys updates key", func(t *testing.T) {
		mtm := &target.MockTargetManager{}
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		client.On("FindSSHKeysForTarget", mock.Anything, mock.Anything).Return(
			&apapproto.SSHKeyResponse{PrivateKeyPaths: &apapproto.StringArray{Values: []string{"/json-key"}}}, nil,
		)

		tgtToUpdate := engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{{}}}
		mtm.On("GetTarget", testTargetName).Return(&tgtToUpdate, nil)
		mtm.On("UpdateTarget", testTargetName, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
			fields := args.Get(1).(*engine_target.UpdateTargetFields)
			updated := fields.UpdatedTarget.(*engine_target.SSHTarget)
			assert.Len(t, updated.Jumps, 1)
			assert.Equal(t, "/json-key", updated.Jumps[0].PrivateKeyFilename)
		})

		cmd := newUpdateCommand(cc, mtm)
		cmd.SetArgs([]string{
			testTargetName,
			"--host", `{"type":"ssh","value":{"jumps":[{"host":"json-host","port":22,"username":"json-user","host_key_policy":"strict","authentication_method":"key"}]}}`,
			"--find-keys",
		})
		err := cmd.Execute()
		assert.NoError(t, err)
		mtm.AssertExpectations(t)
		client.AssertExpectations(t)
	})

	t.Run("update with password JSON target rejects embedded key", func(t *testing.T) {
		mtm := &target.MockTargetManager{}
		cc := &mocks.MockAutostartClientConnector{}

		tgtToUpdate := engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{{}}}
		mtm.On("GetTarget", testTargetName).Return(&tgtToUpdate, nil)

		cmd := newUpdateCommand(cc, mtm)
		cmd.SetArgs([]string{
			testTargetName,
			"--host", `{"type":"ssh","value":{"jumps":[{"host":"json-host","port":22,"username":"json-user","private_key_filename":"/json-key","host_key_policy":"strict","authentication_method":"password"}]}}`,
		})
		err := cmd.Execute()

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CliCmdValidationPasswordAuthWithKey, msgErr.Code())
		assert.Equal(t, "json-host", msgErr.Metadata()["host"])
		assert.Equal(t, "/json-key", msgErr.Metadata()["key"])
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		mtm.AssertNotCalled(t, "UpdateTarget")
	})

	t.Run("update with localhost JSON target fails", func(t *testing.T) {
		mtm := &target.MockTargetManager{}
		cc := &mocks.MockAutostartClientConnector{}

		tgtToUpdate := engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{{}}}
		mtm.On("GetTarget", testTargetName).Return(&tgtToUpdate, nil)
		cmd := newUpdateCommand(cc, mtm)
		cmd.SetArgs([]string{testTargetName, "--host", testLocalHostJSONTargetString})
		err := cmd.Execute()

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CommonUnsupportedTargetType, msgErr.Code())
		assert.Equal(t, "unknown", msgErr.Metadata()["targetType"])
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		mtm.AssertNotCalled(t, "UpdateTarget")
	})
}

func TestUpdateAndroidTarget(t *testing.T) {
	t.Run("android protocol accepts device IP address with port", func(t *testing.T) {
		wasEnabled := viper.GetBool(enableAndroidTargetsConfigKey)
		viper.Set(enableAndroidTargetsConfigKey, true)
		t.Cleanup(func() {
			viper.Set(enableAndroidTargetsConfigKey, wasEnabled)
		})

		mtm := &target.MockTargetManager{}
		cc := &mocks.MockAutostartClientConnector{}
		tgtToUpdate := engine_target.AndroidTarget{SerialNumber: "device-123"}
		mtm.On("GetTarget", testTargetName).Return(&tgtToUpdate, nil)
		mtm.On("UpdateTarget", testTargetName, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
			fields := args.Get(1).(*engine_target.UpdateTargetFields)
			updated := fields.UpdatedTarget.(*engine_target.AndroidTarget)
			assert.Equal(t, "device-123", updated.SerialNumber)
			assert.NotNil(t, updated.DeviceIPAddress)
			assert.Equal(t, "android-target.invalid:5555", *updated.DeviceIPAddress)
		})

		cmd := newUpdateCommand(cc, mtm)
		cmd.SetArgs([]string{testTargetName, "--host", "android://device-123@android-target.invalid:5555"})
		err := cmd.Execute()

		assert.NoError(t, err)
		mtm.AssertExpectations(t)
	})

	t.Run("android protocol rejects device IP address with extra colon-delimited fields", func(t *testing.T) {
		wasEnabled := viper.GetBool(enableAndroidTargetsConfigKey)
		viper.Set(enableAndroidTargetsConfigKey, true)
		t.Cleanup(func() {
			viper.Set(enableAndroidTargetsConfigKey, wasEnabled)
		})

		mtm := &target.MockTargetManager{}
		cc := &mocks.MockAutostartClientConnector{}
		tgtToUpdate := engine_target.AndroidTarget{SerialNumber: "device-123"}
		mtm.On("GetTarget", testTargetName).Return(&tgtToUpdate, nil)

		cmd := newUpdateCommand(cc, mtm)
		cmd.SetArgs([]string{testTargetName, "--host", "android://device-123@android-target.invalid:5555:extra:field"})
		err := cmd.Execute()

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EngineTargetConfigInvalidHostFormat, msgErr.Code())
		assert.Equal(t, "device-123@android-target.invalid:5555:extra:field", msgErr.Metadata()["hostAddress"])
		mtm.AssertNotCalled(t, "UpdateTarget")
	})
}

func TestTargetUpdateHostDescription(t *testing.T) {
	wasEnabled := viper.GetBool(enableAndroidTargetsConfigKey)
	t.Cleanup(func() {
		viper.Set(enableAndroidTargetsConfigKey, wasEnabled)
	})

	viper.Set(enableAndroidTargetsConfigKey, false)
	assert.NotContains(t, targetUpdateHostDescription(), "android://serial[@host]")

	viper.Set(enableAndroidTargetsConfigKey, true)
	assert.Contains(t, targetUpdateHostDescription(), "[user@]host[:port][:private_ssh_key_path][:auth=key|password]")
	assert.Contains(t, targetUpdateHostDescription(), "android://serial[@host]")
}

func TestUpdateHostKeyPolicy(t *testing.T) {
	tgtToUpdate := engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{
		{
			Host:               "1.2.3.4",
			Port:               22,
			Username:           "user",
			PrivateKeyFilename: "/home/user/.ssh/id_ed25519",
			HostKeyPolicy:      engine_target.RejectHostKeyIfMissing,
		},
		{
			Host:               "5.6.7.8",
			Port:               22,
			Username:           "user",
			PrivateKeyFilename: "/home/user/.ssh/id_ed25519",
			HostKeyPolicy:      engine_target.IgnoreHostKey,
		},
	}}

	t.Run("host key policy is updated", func(t *testing.T) {
		mtm := &target.MockTargetManager{}
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		mtm.On("GetTarget", testTargetName).Return(&tgtToUpdate, nil)
		mtm.On("UpdateTarget", testTargetName, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
			tgt := args.Get(1).(*engine_target.UpdateTargetFields)
			expectedTarget := tgtToUpdate
			expectedTarget.Jumps[0].HostKeyPolicy = engine_target.AcceptNewHost
			expectedTarget.Jumps[1].HostKeyPolicy = engine_target.AcceptNewHost
			updatedTargetFields := engine_target.UpdateTargetFields{UpdatedTarget: &expectedTarget}
			assert.True(t, reflect.DeepEqual(&tgt.UpdatedTarget, &updatedTargetFields.UpdatedTarget))
		})
		cmd := newUpdateCommand(cc, mtm)
		cmd.SetArgs([]string{testTargetName, "--host-key-policy", "accept-new"})
		err := cmd.Execute()
		assert.NoError(t, err)
		mtm.AssertExpectations(t)
	})
}
