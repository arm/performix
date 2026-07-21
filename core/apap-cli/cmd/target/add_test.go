// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/ssh"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

const testTargetName = "SUPER_SECRET_TEST_DEVICE"
const testTargetUser = "SUPER_SECRET_USER"
const testTargetKey = "SUPER_SECRET_PRIVATE_KEY"
const testAndroidSerialNumber = "device-123"
const testAndroidDeviceIPAddress = "android-target.invalid:5555"

const testLocalHostJSONTargetString = `{"type": "local"}`
const testJSONTargetString = `{"default":true,"type":"ssh","value":{"jumps":[
{"host":"1.1.1.1","host_key_policy":"strict","port":11,"private_key_filename":"key1","username":"user1"},
{"host":"2.2.2.2","host_key_policy":"strict","private_key_filename":"key2","username":"user2"}]}}`
const testJSONAndroidTargetString = `{"type":"android","value":{"serial_number":"` + testAndroidSerialNumber + `","device_ip_address":"` + testAndroidDeviceIPAddress + `"}}`

func GetTestJSONTarget() engine_target.SSHTarget {
	return engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{
		{Host: "1.1.1.1", HostKeyPolicy: engine_target.ValidHostKeyPolicyValues["strict"], Port: 11, PrivateKeyFilename: "key1", Username: "user1"},
		{Host: "2.2.2.2", HostKeyPolicy: engine_target.ValidHostKeyPolicyValues["strict"], Port: 22, PrivateKeyFilename: "key2", Username: "user2"},
	}}
}

func TestAddCommandMissingHost(t *testing.T) {
	mtm := target.MockTargetManager{}
	mkp := ssh.MockSSHKeyProvisioner{}
	mkp.On("CreateSSHKeyPair", mock.Anything, mock.Anything).Return("foo", nil)
	cmd := newAddCommand(nil, &mtm, &mkp)
	cmd.SetArgs([]string{"--name", "test"})

	_, err := cmd.ExecuteC()

	require.Error(t, err)

	expectedErr := "accepts 1 arg(s), received 0"
	assert.Equal(t, expectedErr, err.Error())
}

func TestAddCommandInvalidArguments(t *testing.T) {
	mtm := target.MockTargetManager{}
	mkp := ssh.MockSSHKeyProvisioner{}
	cmd := newAddCommand(nil, &mtm, &mkp)

	testCases := []struct {
		name        string
		args        []string
		expectedErr string
	}{
		{
			name:        "Too many arguments",
			args:        []string{"hostname.com", "anotherhostname.com", "yetanotherhostname.com"},
			expectedErr: "accepts 1 arg(s), received 3",
		},
		{
			name:        "Missing name",
			args:        []string{"hostname.com", "--name"},
			expectedErr: "flag needs an argument: --name",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd.SetArgs(tc.args)
			_, err := cmd.ExecuteC()

			require.Error(t, err)

			assert.Equal(t, tc.expectedErr, err.Error())
		})
	}
}

func TestAddCommandValidArguments(t *testing.T) {
	mtm := target.MockTargetManager{}
	mkp := ssh.MockSSHKeyProvisioner{}
	var blah []byte
	mkp.On("CreateSSHKeyPair", mock.Anything, mock.Anything).Return("foo", nil)
	mkp.On("ReadPublicKey", mock.Anything).Return(blah, nil)
	mkp.On("ProvisionPublicKeyWithPassword", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mtm.On("AddTarget", testTargetName, mock.Anything).Return(nil)
	mtm.On("SetDefaultTarget", testTargetName).Return(nil)
	cmd := newAddCommand(nil, &mtm, &mkp)
	cmd.SetArgs([]string{testTargetUser + "@111.111.111.111:1111:" + testTargetKey, "--name", testTargetName, "--default"})

	_, err := cmd.ExecuteC()
	assert.NoError(t, err)
}

func TestAddCommandTargetProtocols(t *testing.T) {
	t.Run("explicit ssh protocol succeeds", func(t *testing.T) {
		mtm := target.MockTargetManager{}
		mtm.On("AddTarget", testTargetName, mock.Anything).Return(nil)

		cmd := newAddCommand(nil, &mtm, &ssh.MockSSHKeyProvisioner{})
		cmd.SetArgs([]string{"ssh://" + testTargetUser + "@111.111.111.111:1111:" + testTargetKey, "--name", testTargetName})

		_, err := cmd.ExecuteC()
		require.NoError(t, err)
		mtm.AssertExpectations(t)
	})

	t.Run("ssh protocol on jump succeeds", func(t *testing.T) {
		mtm := target.MockTargetManager{}
		mtm.On("AddTarget", testTargetName, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
			tgt := args.Get(1).(*engine_target.SSHTarget)
			require.Len(t, tgt.Jumps, 2)
			assert.Equal(t, "222.222.222.222", tgt.Jumps[0].Host)
			assert.Equal(t, testTargetUser, tgt.Jumps[0].Username)
		})

		cmd := newAddCommand(nil, &mtm, &ssh.MockSSHKeyProvisioner{})
		cmd.SetArgs([]string{
			"ssh://" + testTargetUser + "@111.111.111.111",
			"--jump", "ssh://" + testTargetUser + "@222.222.222.222",
			"--name", testTargetName,
		})

		_, err := cmd.ExecuteC()
		require.NoError(t, err)
		mtm.AssertExpectations(t)
	})

	t.Run("jump without protocol still succeeds", func(t *testing.T) {
		mtm := target.MockTargetManager{}
		mtm.On("AddTarget", testTargetName, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
			tgt := args.Get(1).(*engine_target.SSHTarget)
			require.Len(t, tgt.Jumps, 2)
			assert.Equal(t, "222.222.222.222", tgt.Jumps[0].Host)
			assert.Equal(t, testTargetUser, tgt.Jumps[0].Username)
		})

		cmd := newAddCommand(nil, &mtm, &ssh.MockSSHKeyProvisioner{})
		cmd.SetArgs([]string{
			"ssh://" + testTargetUser + "@111.111.111.111",
			"--jump", testTargetUser + "@222.222.222.222",
			"--name", testTargetName,
		})

		_, err := cmd.ExecuteC()
		require.NoError(t, err)
		mtm.AssertExpectations(t)
	})

	t.Run("unsupported jump protocol fails", func(t *testing.T) {
		cmd := newAddCommand(nil, &target.MockTargetManager{}, &ssh.MockSSHKeyProvisioner{})
		cmd.SetArgs([]string{
			"ssh://" + testTargetUser + "@111.111.111.111",
			"--jump", "android://device-123",
			"--name", testTargetName,
		})

		_, err := cmd.ExecuteC()

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CommonUnsupportedTargetType, msgErr.Code())
		assert.Equal(t, "android", msgErr.Metadata()["targetType"])
	})

	t.Run("android protocol with serial number succeeds", func(t *testing.T) {
		viper.Set(enableAndroidTargetsConfigKey, true)

		mtm := target.MockTargetManager{}
		tgt := engine_target.AndroidTarget{SerialNumber: testAndroidSerialNumber}
		mtm.On("AddTarget", testTargetName, &tgt).Return(nil)

		cmd := newAddCommand(nil, &mtm, &ssh.MockSSHKeyProvisioner{})
		cmd.SetArgs([]string{"android://" + testAndroidSerialNumber, "--name", testTargetName})

		_, err := cmd.ExecuteC()
		require.NoError(t, err)
		mtm.AssertExpectations(t)
	})

	t.Run("android protocol with device host succeeds", func(t *testing.T) {
		viper.Set(enableAndroidTargetsConfigKey, true)

		mtm := target.MockTargetManager{}
		deviceIPAddress := testAndroidDeviceIPAddress
		tgt := engine_target.AndroidTarget{SerialNumber: testAndroidSerialNumber, DeviceIPAddress: &deviceIPAddress}
		mtm.On("AddTarget", testTargetName, &tgt).Return(nil)

		cmd := newAddCommand(nil, &mtm, &ssh.MockSSHKeyProvisioner{})
		cmd.SetArgs([]string{"android://" + testAndroidSerialNumber + "@" + testAndroidDeviceIPAddress, "--name", testTargetName})

		_, err := cmd.ExecuteC()
		require.NoError(t, err)
		mtm.AssertExpectations(t)
	})

	t.Run("android protocol fails when Android targets are disabled", func(t *testing.T) {
		viper.Set(enableAndroidTargetsConfigKey, false)
		t.Cleanup(func() {
			viper.Set(enableAndroidTargetsConfigKey, true)
		})

		cmd := newAddCommand(nil, &target.MockTargetManager{}, &ssh.MockSSHKeyProvisioner{})
		cmd.SetArgs([]string{"android://" + testAndroidSerialNumber, "--name", testTargetName})

		_, err := cmd.ExecuteC()

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CommonUnsupportedTargetType, msgErr.Code())
		assert.Equal(t, "android", msgErr.Metadata()["targetType"])
	})

	t.Run("unsupported protocol fails", func(t *testing.T) {
		cmd := newAddCommand(nil, &target.MockTargetManager{}, &ssh.MockSSHKeyProvisioner{})
		cmd.SetArgs([]string{"serial://device-123", "--name", testTargetName})

		_, err := cmd.ExecuteC()

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CommonUnsupportedTargetType, msgErr.Code())
		assert.Equal(t, "serial", msgErr.Metadata()["targetType"])
	})
}

func TestAddCommandAddTargetFailsOnError(t *testing.T) {
	var blah []byte
	mtm := target.MockTargetManager{}
	mtm.On("AddTarget", testTargetName, mock.Anything).Return(errors.New("rekt"))
	mkp := ssh.MockSSHKeyProvisioner{}
	mkp.On("CreateSSHKeyPair", mock.Anything, mock.Anything).Return("foo", nil)
	mkp.On("ReadPublicKey", mock.Anything).Return(blah, nil)
	mkp.On("ProvisionPublicKeyWithPassword", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	cmd := newAddCommand(nil, &mtm, &mkp)
	cmd.SetArgs([]string{testTargetUser + "@111.111.111.111:1111:" + testTargetKey, "--name", testTargetName})

	_, err := cmd.ExecuteC()
	assert.Error(t, err)
	assert.ErrorContains(t, err, "rekt")
}

func TestAddCommandSetDefaultTargetFails(t *testing.T) {
	var blah []byte
	mtm := target.MockTargetManager{}
	mtm.On("AddTarget", testTargetName, mock.Anything).Return(nil)
	mtm.On("SetDefaultTarget", testTargetName).Return(errors.New("rekt"))
	mkp := ssh.MockSSHKeyProvisioner{}
	mkp.On("CreateSSHKeyPair", mock.Anything, mock.Anything).Return("foo", nil)
	mkp.On("ReadPublicKey", mock.Anything).Return(blah, nil)
	mkp.On("ProvisionPublicKeyWithPassword", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	cmd := newAddCommand(nil, &mtm, &mkp)
	cmd.SetArgs([]string{testTargetUser + "@111.111.111.111:1111:" + testTargetKey, "--name", testTargetName, "--default"})

	_, err := cmd.ExecuteC()
	assert.Error(t, err)
	assert.ErrorContains(t, err, "rekt")
}

func TestAddCommandAddRandomTargetName(t *testing.T) {
	var blah []byte
	mtm := target.MockTargetManager{}
	mtm.On("AddTarget", mock.Anything, mock.Anything).Return(nil)
	mtm.On("ReadTargetConfig").Return(&engine_target.TargetConfig{Default: "default", Targets: make(map[string]engine_target.Target)}, nil)
	mkp := ssh.MockSSHKeyProvisioner{}
	mkp.On("CreateSSHKeyPair", mock.Anything, mock.Anything).Return("foo", nil)
	mkp.On("ReadPublicKey", mock.Anything).Return(blah, nil)
	mkp.On("ProvisionPublicKeyWithPassword", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	cmd := newAddCommand(nil, &mtm, &mkp)
	cmd.SetArgs([]string{testTargetUser + "@111.111.111.111:" + testTargetKey})

	_, err := cmd.ExecuteC()
	assert.NoError(t, err)
}

func TestAddCommandFindPrivateKey(t *testing.T) {
	t.Run("add with --find-keys fails when connector fails", func(t *testing.T) {
		expectedError := errors.New("rekt")
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(nil, expectedError)

		mtm := target.MockTargetManager{}
		mtm.On("AddTarget", testTargetName, mock.Anything).Return(nil)
		mtm.On("SetActiveTarget", testTargetName).Return(nil)
		mkp := ssh.MockSSHKeyProvisioner{}
		mkp.On("CreateSSHKeyPair", mock.Anything, mock.Anything).Return("foo", nil)
		cmd := newAddCommand(cc, &mtm, &mkp)
		cmd.SetArgs([]string{testTargetUser + "@111.111.111.111", "--name", testTargetName, "--find-keys"})

		err := cmd.Execute()
		assert.ErrorContains(t, err, "rekt")
	})

	t.Run("add with --find-keys fails when findKey fails", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		client.On("FindSSHKeysForTarget", mock.Anything, mock.Anything).Return(
			&apapproto.SSHKeyResponse{Error: message.BuildErrorChain(errors.New("rekt"))},
			nil,
		)

		mtm := target.MockTargetManager{}
		mtm.On("AddTarget", testTargetName, mock.Anything).Return(nil)
		mtm.On("SetActiveTarget", testTargetName).Return(nil)
		mkp := ssh.MockSSHKeyProvisioner{}
		mkp.On("CreateSSHKeyPair", mock.Anything, mock.Anything).Return("foo", nil)
		cmd := newAddCommand(cc, &mtm, &mkp)
		cmd.SetArgs([]string{testTargetUser + "@111.111.111.111", "--name", testTargetName, "--find-keys"})

		err := cmd.Execute()
		assert.ErrorContains(t, err, "rekt")
	})

	t.Run("add with --find-keys succeeds when findKey returns a key", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		client.On("FindSSHKeysForTarget", mock.Anything, mock.Anything).Return(&apapproto.SSHKeyResponse{PrivateKeyPaths: &apapproto.StringArray{Values: []string{"/thePath"}}}, nil)

		mtm := target.MockTargetManager{}
		mtm.On("AddTarget", testTargetName, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
			tgt := args.Get(1).(*engine_target.SSHTarget)
			expectedTarget := engine_target.SSHTarget{
				Jumps: []engine_target.SSHHostConfig{
					{Host: "111.111.111.111",
						Port:               22,
						Username:           testTargetUser,
						PrivateKeyFilename: "/thePath",
						HostKeyPolicy:      engine_target.AskNewHost},
				},
			}
			assert.True(t, reflect.DeepEqual(*tgt, expectedTarget))
		})
		mtm.On("SetActiveTarget", testTargetName).Return(nil)
		mkp := ssh.MockSSHKeyProvisioner{}
		mkp.On("CreateSSHKeyPair", mock.Anything, mock.Anything).Return("foo", nil)
		cmd := newAddCommand(cc, &mtm, &mkp)
		cmd.SetArgs([]string{testTargetUser + "@111.111.111.111", "--name", testTargetName, "--find-keys"})

		err := cmd.Execute()
		assert.NoError(t, err)
	})

}

func TestAddCommandHostKeyPolicyValues(t *testing.T) {
	validValues := []string{"ask", "strict", "accept-new", "ignore"}
	invalidValues := []string{"", "foo", "true", "false", "yess", "none"}
	var blah []byte

	t.Run("default host key policy is ask", func(t *testing.T) {
		mtm := &target.MockTargetManager{}
		mtm.On("AddTarget", testTargetName, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
			tgt := args.Get(1).(*engine_target.SSHTarget)
			require.Len(t, tgt.Jumps, 1)
			assert.Equal(t, engine_target.AskNewHost, tgt.Jumps[0].HostKeyPolicy)
		})
		mkp := &ssh.MockSSHKeyProvisioner{}
		mkp.On("CreateSSHKeyPair", mock.Anything, mock.Anything).Return("foo", nil)
		mkp.On("ReadPublicKey", mock.Anything).Return(blah, nil)
		mkp.On("ProvisionPublicKeyWithPassword", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		cmd := newAddCommand(nil, mtm, mkp)
		cmd.SetArgs([]string{testTargetUser + "@111.111.111.111:" + testTargetKey, "--name", testTargetName})

		err := cmd.Execute()
		assert.NoError(t, err)
	})

	t.Run("password provisioning reads stdin when no key provided", func(t *testing.T) {
		origStdin := os.Stdin
		r, w, err := os.Pipe()
		require.NoError(t, err)
		_, err = w.Write([]byte("piped-secret\n"))
		require.NoError(t, err)
		_ = w.Close()
		os.Stdin = r
		t.Cleanup(func() {
			os.Stdin = origStdin
			_ = r.Close()
		})

		mtm := &target.MockTargetManager{}
		mtm.On("AddTarget", mock.Anything, mock.Anything).Return(nil)

		mkp := &ssh.MockSSHKeyProvisioner{}
		mkp.On("CreateSSHKeyPair", mock.Anything, mock.Anything).Return("/tmp/key", nil)
		mkp.On("ReadPublicKey", "/tmp/key.pub").Return([]byte("pub"), nil)
		mkp.On("ProvisionPublicKeyWithPassword", mock.Anything, mock.Anything, mock.Anything, mock.Anything, "piped-secret", "pub").Return(nil)

		cmd := newAddCommand(nil, mtm, mkp)
		cmd.SetArgs([]string{testTargetUser + "@111.111.111.111", "--name", "piped", "--provision-key"})

		err = cmd.Execute()
		require.NoError(t, err)

		assert.Equal(t, "piped-secret", userPassword)
		mtm.AssertExpectations(t)
		mkp.AssertExpectations(t)
	})
	for _, value := range validValues {
		t.Run("valid-"+value, func(t *testing.T) {
			mtm := &target.MockTargetManager{}
			mtm.On("AddTarget", mock.Anything, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
				tgt := args.Get(1).(*engine_target.SSHTarget)
				require.Len(t, tgt.Jumps, 1)
				assert.Equal(t, engine_target.ValidHostKeyPolicyValues[value], tgt.Jumps[0].HostKeyPolicy)
			})
			mkp := &ssh.MockSSHKeyProvisioner{}
			mkp.On("CreateSSHKeyPair", mock.Anything, mock.Anything).Return("foo", nil)
			mkp.On("ReadPublicKey", mock.Anything).Return(blah, nil)
			mkp.On("ProvisionPublicKeyWithPassword", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
			cmd := newAddCommand(nil, mtm, mkp)
			cmd.SetArgs([]string{testTargetUser + "@111.111.111.111:" + testTargetKey, "--name", testTargetName, "--host-key-policy", value})

			err := cmd.Execute()
			assert.NoError(t, err)
		})
	}

	for _, value := range invalidValues {
		t.Run("invalid-"+value, func(t *testing.T) {
			mkp := ssh.MockSSHKeyProvisioner{}
			mkp.On("CreateSSHKeyPair", mock.Anything, mock.Anything).Return("foo", nil)
			mkp.On("ReadPublicKey", mock.Anything).Return(blah, nil)
			mkp.On("ProvisionPublicKeyWithPassword", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
			cmd := newAddCommand(nil, &target.MockTargetManager{}, &mkp)
			cmd.SetArgs([]string{testTargetUser + "@111.111.111.111:" + testTargetKey, "--name", testTargetName, "--host-key-policy", value})

			err := cmd.Execute()
			var msgErr message.Message
			ok := errors.As(err, &msgErr)
			assert.True(t, ok)
			assert.Equal(t, message.CliCmdValidationInvalidFlagValue, msgErr.Code())
			assert.Equal(t, value, msgErr.Metadata()["value"])
			assert.Equal(t, "--host-key-policy", msgErr.Metadata()["flag"])
		})
	}

	t.Run("invalid host key policy is rejected for JSON target", func(t *testing.T) {
		value := "foo"
		cmd := newAddCommand(nil, &target.MockTargetManager{}, &ssh.MockSSHKeyProvisioner{})
		cmd.SetArgs([]string{testJSONTargetString, "--name", testTargetName, "--host-key-policy", value})

		err := cmd.Execute()
		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CliCmdValidationInvalidFlagValue, msgErr.Code())
		assert.Equal(t, value, msgErr.Metadata()["value"])
		assert.Equal(t, "--host-key-policy", msgErr.Metadata()["flag"])
	})

	t.Run("--find-keys and --provision-key are mutually exclusive", func(t *testing.T) {
		mtm := target.MockTargetManager{}
		mtm.On("ReadTargetConfig").Return(&engine_target.TargetConfig{
			Default: "default",
			Targets: make(map[string]engine_target.Target),
		}, nil)

		cmd := newAddCommand(nil, &mtm, &ssh.MockSSHKeyProvisioner{})
		cmd.SetArgs([]string{testTargetUser + "@111.111.111.111", "--find-keys", "--provision-key"})

		err := cmd.Execute()

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CliCmdValidationMutuallyExclusiveFlags, msgErr.Code())
		assert.Equal(t, "--find-keys", msgErr.Metadata()["flag1"])
		assert.Equal(t, "--provision-key", msgErr.Metadata()["flag2"])
	})

	t.Run("--find-keys rejected when key embedded", func(t *testing.T) {
		mtm := target.MockTargetManager{}
		mtm.On("ReadTargetConfig").Return(&engine_target.TargetConfig{
			Default: "default",
			Targets: make(map[string]engine_target.Target),
		}, nil)

		cmd := newAddCommand(nil, &mtm, &ssh.MockSSHKeyProvisioner{})
		cmd.SetArgs([]string{testTargetUser + "@111.111.111.111:/path/to/key", "--find-keys"})

		err := cmd.Execute()

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CliCmdValidationMutuallyExclusiveFlags, msgErr.Code())
		assert.Equal(t, "--find-keys", msgErr.Metadata()["flag1"])
		assert.Equal(t, "embedded-private-key", msgErr.Metadata()["flag2"])
	})

	t.Run("--provision-key rejected when key embedded", func(t *testing.T) {
		mtm := target.MockTargetManager{}
		mtm.On("ReadTargetConfig").Return(&engine_target.TargetConfig{
			Default: "default",
			Targets: make(map[string]engine_target.Target),
		}, nil)

		cmd := newAddCommand(nil, &mtm, &ssh.MockSSHKeyProvisioner{})
		cmd.SetArgs([]string{testTargetUser + "@111.111.111.111:/path/to/key", "--provision-key"})

		err := cmd.Execute()

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CliCmdValidationMutuallyExclusiveFlags, msgErr.Code())
		assert.Equal(t, "--provision-key", msgErr.Metadata()["flag1"])
		assert.Equal(t, "embedded-private-key", msgErr.Metadata()["flag2"])
	})

	t.Run("auth methods apply to jumps and target", func(t *testing.T) {
		mtm := target.MockTargetManager{}
		mtm.On("AddTarget", testTargetName, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
			tgt := args.Get(1).(*engine_target.SSHTarget)
			require.Len(t, tgt.Jumps, 2)
			assert.Equal(t, engine_target.SSHAuthMethodPassword, tgt.Jumps[0].AuthMethod)
			assert.Equal(t, engine_target.SSHAuthMethodKey, tgt.Jumps[1].AuthMethod)
		})
		mkp := ssh.MockSSHKeyProvisioner{}
		cmd := newAddCommand(nil, &mtm, &mkp)
		cmd.SetArgs([]string{
			testTargetUser + "@111.111.111.111:auth=key",
			"--jump", "jumpuser@222.222.222.222:auth=password",
			"--name", testTargetName,
		})

		err := cmd.Execute()
		assert.NoError(t, err)
	})

	t.Run("invalid auth method", func(t *testing.T) {
		mtm := target.MockTargetManager{}
		cmd := newAddCommand(nil, &mtm, &ssh.MockSSHKeyProvisioner{})
		cmd.SetArgs([]string{
			testTargetUser + "@111.111.111.111:auth=bad_method",
			"--name", testTargetName,
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
		mtm := target.MockTargetManager{}
		cmd := newAddCommand(nil, &mtm, &ssh.MockSSHKeyProvisioner{})
		cmd.SetArgs([]string{
			testTargetUser + "@111.111.111.111:auth=password",
			"--find-keys",
			"--name", testTargetName,
		})

		err := cmd.Execute()

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CliCmdValidationPasswordAuthUnsupportedFlag, msgErr.Code())
		assert.Equal(t, "--find-keys", msgErr.Metadata()["flag"])
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("password auth and --provision-key are mutually exclusive", func(t *testing.T) {
		mtm := target.MockTargetManager{}
		cmd := newAddCommand(nil, &mtm, &ssh.MockSSHKeyProvisioner{})
		cmd.SetArgs([]string{
			testTargetUser + "@111.111.111.111:auth=password",
			"--provision-key",
			"--name", testTargetName,
		})

		err := cmd.Execute()

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CliCmdValidationPasswordAuthUnsupportedFlag, msgErr.Code())
		assert.Equal(t, "--provision-key", msgErr.Metadata()["flag"])
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("password auth rejected with key", func(t *testing.T) {
		mtm := target.MockTargetManager{}
		cmd := newAddCommand(nil, &mtm, &ssh.MockSSHKeyProvisioner{})
		cmd.SetArgs([]string{
			testTargetUser + "@111.111.111.111:/path/to/key:auth=password",
			"--name", testTargetName,
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

func TestAddJSONStringTarget(t *testing.T) {
	t.Run("add with valid JSON target succeeds", func(t *testing.T) {
		mtm := target.MockTargetManager{}
		tgt := GetTestJSONTarget()
		mtm.On("AddTarget", mock.Anything, &tgt).Return(nil)
		mtm.On("ReadTargetConfig").Return(&engine_target.TargetConfig{
			Default: "default",
			Targets: make(map[string]engine_target.Target),
		}, nil)

		mkp := ssh.MockSSHKeyProvisioner{}
		cmd := newAddCommand(nil, &mtm, &mkp)
		cmd.SetArgs([]string{testJSONTargetString})

		_, err := cmd.ExecuteC()
		require.NoError(t, err)
		mtm.AssertExpectations(t)
	})

	t.Run("add with valid Android JSON target succeeds", func(t *testing.T) {
		viper.Set(enableAndroidTargetsConfigKey, true)

		mtm := target.MockTargetManager{}
		deviceIPAddress := testAndroidDeviceIPAddress
		tgt := engine_target.AndroidTarget{SerialNumber: testAndroidSerialNumber, DeviceIPAddress: &deviceIPAddress}
		mtm.On("AddTarget", testTargetName, &tgt).Return(nil)

		mkp := ssh.MockSSHKeyProvisioner{}
		cmd := newAddCommand(nil, &mtm, &mkp)
		cmd.SetArgs([]string{testJSONAndroidTargetString, "--name", testTargetName})

		_, err := cmd.ExecuteC()
		require.NoError(t, err)
		mtm.AssertExpectations(t)
	})

	t.Run("add with Android JSON target fails when Android targets are disabled", func(t *testing.T) {
		viper.Set(enableAndroidTargetsConfigKey, false)
		t.Cleanup(func() {
			viper.Set(enableAndroidTargetsConfigKey, true)
		})

		cmd := newAddCommand(nil, &target.MockTargetManager{}, &ssh.MockSSHKeyProvisioner{})
		cmd.SetArgs([]string{testJSONAndroidTargetString, "--name", testTargetName})

		_, err := cmd.ExecuteC()

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CommonUnsupportedTargetType, msgErr.Code())
		assert.Equal(t, "android", msgErr.Metadata()["targetType"])
	})

	t.Run("add with localhost JSON target fails", func(t *testing.T) {
		mtm := target.MockTargetManager{}
		mkp := ssh.MockSSHKeyProvisioner{}
		cmd := newAddCommand(nil, &mtm, &mkp)
		cmd.SetArgs([]string{testLocalHostJSONTargetString})

		_, err := cmd.ExecuteC()

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CommonUnsupportedTargetType, msgErr.Code())
		assert.Equal(t, "local", msgErr.Metadata()["targetType"])
	})
}

func TestAddCommandJSONOutput(t *testing.T) {
	t.Run("empty struct output on success with --json", func(t *testing.T) {
		mtm := target.MockTargetManager{}
		mkp := ssh.MockSSHKeyProvisioner{}
		var blah []byte
		mkp.On("CreateSSHKeyPair", mock.Anything, mock.Anything).Return("foo", nil)
		mkp.On("ReadPublicKey", mock.Anything).Return(blah, nil)
		mkp.On("ProvisionPublicKeyWithPassword", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		mtm.On("AddTarget", testTargetName, mock.Anything).Return(nil)
		mtm.On("SetDefaultTarget", testTargetName).Return(nil)

		cmd := newAddCommand(nil, &mtm, &mkp)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{testTargetUser + "@111.111.111.111:1111:" + testTargetKey, "--name", testTargetName, "--default", "--json"})

		_, err := cmd.ExecuteC()
		assert.NoError(t, err)
		assert.True(t, utils.IsValidJSON(cmdBuf.String()))
		assert.Contains(t, cmdBuf.String(), `"code":"0"`)
		assert.Contains(t, cmdBuf.String(), `"data":{}`)
		assert.Contains(t, cmdBuf.String(), `"error":{"message_code":""`)
	})
}
