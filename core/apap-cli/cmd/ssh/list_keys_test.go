// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package ssh

import (
	"bytes"
	"errors"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestListCommand(t *testing.T) {
	t.Run("error is raised when invalid arguments specified", func(t *testing.T) {

		cmd := NewListKeysCmd(nil)
		cmd.SetArgs([]string{"abcdef123456"})

		err := cmd.Execute()

		assert.Error(t, err)
		assert.ErrorContains(t, err, "accepts 0 arg(s), received 1")
	})

	t.Run("connector error is reflected in output", func(t *testing.T) {

		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(nil, errors.New("rekt"))
		cmd := NewListKeysCmd(cc)
		cmd.SetArgs([]string{})

		err := cmd.Execute()

		assert.ErrorContains(t, err, "rekt")
	})

	t.Run("api error is reflected in output", func(t *testing.T) {

		cc := &mocks.MockAutostartClientConnector{}
		client := apapprotomocks.NewApapClient(t)
		cc.SetClient(client, nil)
		client.On("ListPrivateSSHKeys", mock.Anything, mock.Anything).Return(nil, errors.New("rekt"))
		cmd := NewListKeysCmd(cc)
		cmd.SetArgs([]string{})

		err := cmd.Execute()

		assert.ErrorContains(t, err, "rekt")
	})

	t.Run("no keys output is correct", func(t *testing.T) {

		cc := &mocks.MockAutostartClientConnector{}
		client := apapprotomocks.NewApapClient(t)
		cc.SetClient(client, nil)
		client.On("ListPrivateSSHKeys", mock.Anything, mock.Anything).Return(&apapproto.PrivateSSHKeyListing{Keys: []*apapproto.PrivateSSHKey{}}, nil)
		cmd := NewListKeysCmd(cc)
		cmd.SetArgs([]string{})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)

		err := cmd.Execute()

		assert.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), "No valid SSH private keys found.")
	})

	t.Run("valid keys are shown in output", func(t *testing.T) {

		cc := &mocks.MockAutostartClientConnector{}
		client := apapprotomocks.NewApapClient(t)
		cc.SetClient(client, nil)
		client.On("ListPrivateSSHKeys", mock.Anything, mock.Anything).Return(&apapproto.PrivateSSHKeyListing{
			Keys: []*apapproto.PrivateSSHKey{
				{Path: ".ssh/keyA"},
				{Path: ".ssh/keyB"},
			},
		}, nil)
		cmd := NewListKeysCmd(cc)
		cmd.SetArgs([]string{})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)

		err := cmd.Execute()

		assert.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), "SSH private keys found")
		assert.Contains(t, cmdBuf.String(), ".ssh/keyA")
		assert.Contains(t, cmdBuf.String(), ".ssh/keyB")
	})

	t.Run("passphrase status is shown per key", func(t *testing.T) {

		cc := &mocks.MockAutostartClientConnector{}
		client := apapprotomocks.NewApapClient(t)
		cc.SetClient(client, nil)
		client.On("ListPrivateSSHKeys", mock.Anything, mock.Anything).Return(&apapproto.PrivateSSHKeyListing{
			Keys: []*apapproto.PrivateSSHKey{
				{Path: ".ssh/keyA", HasPassphrase: true},
				{Path: ".ssh/keyB", HasPassphrase: false},
			},
		}, nil)
		cmd := NewListKeysCmd(cc)
		cmd.SetArgs([]string{})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)

		err := cmd.Execute()

		assert.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), ".ssh/keyA (passphrase: yes)")
		assert.Contains(t, cmdBuf.String(), ".ssh/keyB (passphrase: no)")
	})

	t.Run("json output includes keys", func(t *testing.T) {

		cc := &mocks.MockAutostartClientConnector{}
		client := apapprotomocks.NewApapClient(t)
		cc.SetClient(client, nil)
		client.On("ListPrivateSSHKeys", mock.Anything, mock.Anything).Return(&apapproto.PrivateSSHKeyListing{
			Keys: []*apapproto.PrivateSSHKey{
				{Path: ".ssh/keyA", HasPassphrase: true},
			},
		}, nil)
		cmd := NewListKeysCmd(cc)
		cmd.SetArgs([]string{})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		viper.Set("json", true)
		t.Cleanup(func() { viper.Set("json", false) })

		err := cmd.Execute()

		assert.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), "\"keys\"")
		assert.Contains(t, cmdBuf.String(), "\"path\":\".ssh/keyA\"")
		assert.Contains(t, cmdBuf.String(), "\"has_passphrase\":true")
	})
}
