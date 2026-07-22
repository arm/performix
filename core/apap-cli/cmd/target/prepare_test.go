// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	targetloginmocks "github.com/Arm-Debug/apap-cli/apap-cli/service/targetlogin/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestTargetPrepareCommand(t *testing.T) {
	rektError := errors.New("rekt")

	t.Run("returns error with the wrong number of args", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		cmd := newTargetPrepareCommand(&mocks.MockAutostartClientConnector{}, &target.MockTargetManager{}, loginService)
		cmd.SetArgs([]string{"anArg"})

		err := cmd.Execute()
		assert.ErrorContains(t, err, "accepts 0 arg(s), received 1")
	})

	tgt := &engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{{}}}

	t.Run("returns error when client connector fails", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(nil, rektError)

		mtm := &target.MockTargetManager{}
		mtm.On("GetTarget", mock.Anything).Return(tgt, nil)

		cmd := newTargetPrepareCommand(cc, mtm, loginService)

		err := cmd.Execute()
		assert.ErrorContains(t, err, "rekt")
	})

	t.Run("returns error when getting target fails", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		mtm := &target.MockTargetManager{}
		mtm.On("GetTarget", mock.Anything).Return(tgt, rektError)

		cmd := newTargetPrepareCommand(&mocks.MockAutostartClientConnector{}, mtm, loginService)

		err := cmd.Execute()
		assert.ErrorContains(t, err, "rekt")
	})

	t.Run("propagates target login error", func(t *testing.T) {
		loginErr := errors.New("LoginToTarget failed")
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, loginErr)
		mtm := &target.MockTargetManager{}
		mtm.On("GetTarget", mock.Anything).Return(tgt, nil)

		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		cmd := newTargetPrepareCommand(cc, mtm, loginService)
		utils.SetPersistentFlags(cmd)

		err := cmd.Execute()
		assert.ErrorIs(t, err, loginErr)
	})

	t.Run("returns error when preparation fails", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		mtm := &target.MockTargetManager{}
		mtm.On("GetTarget", mock.Anything).Return(tgt, nil)

		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		client.On("TargetPrepare", mock.Anything, mock.Anything).Return(&apapproto.TargetPrepareResponse{}, rektError)

		cmd := newTargetPrepareCommand(cc, mtm, loginService)
		utils.SetPersistentFlags(cmd)

		err := cmd.Execute()
		assert.ErrorContains(t, err, "rekt")
	})

	t.Run("success when preparation succeeds", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		mtm := &target.MockTargetManager{}
		mtm.On("GetTarget", mock.Anything).Return(tgt, nil)

		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		client.On("TargetPrepare", mock.Anything, mock.Anything).Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_DEPLOYED}, nil)

		cmd := newTargetPrepareCommand(cc, mtm, loginService)
		utils.SetPersistentFlags(cmd)

		err := cmd.Execute()
		assert.NoError(t, err)
	})

	t.Run("success when preparation succeeds but no action was taken", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		mtm := &target.MockTargetManager{}
		mtm.On("GetTarget", mock.Anything).Return(tgt, nil)

		msg := message.New(message.CliCmdTargetPrepareAlreadyPrepared)

		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		client.On("TargetPrepare", mock.Anything, mock.Anything).Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_NO_ACTION}, msg)

		cmd := newTargetPrepareCommand(cc, mtm, loginService)
		utils.SetPersistentFlags(cmd)

		err := cmd.Execute()
		assert.ErrorIs(t, err, msg)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("success when preparation returns deploy required", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		mtm := &target.MockTargetManager{}
		mtm.On("GetTarget", mock.Anything).Return(tgt, nil)

		msg := message.New(message.CliCmdTargetPrepareRequired)

		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		client.On("TargetPrepare", mock.Anything, mock.Anything).Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_DEPLOY}, msg)

		cmd := newTargetPrepareCommand(cc, mtm, loginService)
		utils.SetPersistentFlags(cmd)

		err := cmd.Execute()
		assert.ErrorIs(t, err, msg)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("GetTarget is called with the specific target name from flag", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		mtm := &target.MockTargetManager{}
		mtm.On("GetTarget", "specific_target").Return(tgt, nil)

		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		client.On("TargetPrepare", mock.Anything, mock.Anything).Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_DEPLOYED}, nil)

		cmd := newTargetPrepareCommand(cc, mtm, loginService)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"--target", "specific_target"})

		err := cmd.Execute()

		assert.NoError(t, err)
		mtm.AssertCalled(t, "GetTarget", "specific_target")
	})

}
