// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	targetloginmocks "github.com/Arm-Debug/apap-cli/apap-cli/service/targetlogin/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

type mockTargetTester struct {
	mock.Mock
}

var functionalTarget = &engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{{Host: "A", Port: 22}}}
var targetResponse = target.TestTargetResponse{
	ConnectionStatus: target.TargetTestConnection{ConnectionStatus: 1, Error: errors.New("target connection failed")},
}

func (dt *mockTargetTester) TestTarget(ctx context.Context, client apapproto.ApapClient, t engine_target.Target) (target.TestTargetResponse, error) {
	mockArgs := dt.Called(ctx, client, t)
	return mockArgs.Get(0).(target.TestTargetResponse), mockArgs.Error(1)
}

func createMockTargetManager() *target.MockTargetManager {
	mtm := &target.MockTargetManager{}
	mtm.On("GetTarget", mock.Anything).Return(functionalTarget, nil)
	mtm.On("GetDefaultTargetName").Return("functionalTarget", nil)
	return mtm
}

func createMockTargetTester() *mockTargetTester {
	mdt := &mockTargetTester{}
	mdt.Mock.On("TestTarget", mock.Anything, mock.Anything, functionalTarget).Return(targetResponse, nil)
	return mdt
}

func setDefaultLookupFunc() {
	clijson.LookupMsg = func(err error) (*message.CatalogMessage, error) {
		return &message.CatalogMessage{
			Code:        "engine.recipe.run.FAILED",
			Severity:    "Error",
			Message:     "FromCatalog",
			Explanation: "X happened",
			Advice:      "Do Y",
		}, nil
	}
}

func TestTargetsTestCmd(t *testing.T) {
	rektError := errors.New("rekt")

	client := apapprotomocks.NewApapClient(t)
	cc := &mocks.MockAutostartClientConnector{}
	cc.SetClient(client, nil)

	orig := clijson.LookupMsg
	t.Cleanup(func() { clijson.LookupMsg = orig })

	t.Run("no arg fails if no default exists", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		setDefaultLookupFunc()
		mtm := &target.MockTargetManager{}
		mtm.On("GetDefaultTargetName").Return("", rektError)
		mtm.On("GetTarget", mock.Anything).Return(&engine_target.SSHTarget{}, rektError)

		cmd := newTargetTestCmd(cc, mtm, nil, loginService)
		cmd.SetArgs([]string{})
		err := cmd.Execute()
		assert.Equal(t, rektError, err)
	})

	t.Run("no arg uses default and passes", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, functionalTarget, nil)
		setDefaultLookupFunc()
		mtm := &target.MockTargetManager{}
		mtm.On("GetDefaultTargetName").Return("functionalTarget", nil)
		mtm.On("GetTarget", mock.Anything).Return(functionalTarget, nil)
		mdt := createMockTargetTester()

		cmd := newTargetTestCmd(cc, mtm, mdt, loginService)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()
		require.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), "Testing target: functionalTarget")
	})

	t.Run("valid target arg fails if client fails in TestTarget", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, functionalTarget, nil)
		setDefaultLookupFunc()
		mtm := createMockTargetManager()

		mdt := &mockTargetTester{}
		mdt.Mock.On("TestTarget", mock.Anything, mock.Anything, functionalTarget).Return(target.TestTargetResponse{}, rektError)

		cmd := newTargetTestCmd(cc, mtm, mdt, loginService)
		cmd.SetArgs([]string{"functionalTarget"})
		err := cmd.Execute()
		assert.Equal(t, rektError, err)
	})

	t.Run("valid target succeeds", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, functionalTarget, nil)
		setDefaultLookupFunc()
		mtm := createMockTargetManager()
		mdt := createMockTargetTester()

		cmd := newTargetTestCmd(cc, mtm, mdt, loginService)
		cmd.SetArgs([]string{"functionalTarget"})
		err := cmd.Execute()
		require.NoError(t, err)
	})

	t.Run("valid json target succeeds", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, functionalTarget, nil)
		setDefaultLookupFunc()
		mtm := createMockTargetManager()
		mdt := createMockTargetTester()

		cmd := newTargetTestCmd(cc, mtm, mdt, loginService)
		cmd.SetArgs([]string{"{\"type\": \"ssh\", \"value\":{\"jumps\":[{\"host\": \"A\", \"port\": 22}]}}"})
		err := cmd.Execute()
		require.NoError(t, err)
	})

	t.Run("valid target shows expected connection status in json", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, functionalTarget, nil)
		setDefaultLookupFunc()
		mtm := createMockTargetManager()
		mdt := createMockTargetTester()

		cmd := newTargetTestCmd(cc, mtm, mdt, loginService)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"functionalTarget", "--json"})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()

		assert.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), "\"connection\":{\"status\":1,\"error\":{\"message_code\":\"\",\"severity\":\"Error\",\"message\":\"target connection failed\"")
		assert.True(t, utils.IsValidJSON(cmdBuf.String()))
	})

	t.Run("valid target shows expected connection status in stdout on failure", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, functionalTarget, nil)
		clijson.LookupMsg = func(err error) (*message.CatalogMessage, error) {
			return &message.CatalogMessage{
				Code:        "engine.recipe.run.FAILED",
				Severity:    "Error",
				Message:     "target connection failed",
				Explanation: "X happened",
				Advice:      "Do Y",
			}, nil
		}
		mtm := createMockTargetManager()
		mdt := createMockTargetTester()

		cmd := newTargetTestCmd(cc, mtm, mdt, loginService)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"functionalTarget"})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()

		assert.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), "Target Connection - Fail")
		assert.Contains(t, cmdBuf.String(), "target connection failed")
		assert.Contains(t, cmdBuf.String(), "X happened")
		assert.Contains(t, cmdBuf.String(), "Do Y")
	})

	t.Run("valid target shows expected connection status in stdout on success", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, functionalTarget, nil)
		clijson.LookupMsg = func(err error) (*message.CatalogMessage, error) {
			return &message.CatalogMessage{
				Code:        "engine.recipe.run.FAILED",
				Severity:    "Error",
				Message:     "target connection failed",
				Explanation: "X happened",
				Advice:      "Do Y",
			}, nil
		}
		mtm := createMockTargetManager()

		response := target.TestTargetResponse{
			ConnectionStatus: target.TargetTestConnection{ConnectionStatus: apapproto.ConnectionStatus_CONNECTION_STATUS_OK, Error: nil},
		}
		mdt := &mockTargetTester{}
		mdt.On("TestTarget", mock.Anything, mock.Anything, functionalTarget).Return(response, nil)

		cmd := newTargetTestCmd(cc, mtm, mdt, loginService)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"functionalTarget"})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()

		assert.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), "Target Connection - Success")
		assert.Contains(t, cmdBuf.String(), "Test completed with 0 errors.")
	})

	t.Run("valid localhost target shows expected connection status in stdout on success", func(t *testing.T) {
		clijson.LookupMsg = func(err error) (*message.CatalogMessage, error) {
			return &message.CatalogMessage{
				Code:        "engine.recipe.run.FAILED",
				Severity:    "Error",
				Message:     "target connection failed",
				Explanation: "X happened",
				Advice:      "Do Y",
			}, nil
		}
		localTarget := &engine_target.LocalTarget{}
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, localTarget, nil)
		mtm := &target.MockTargetManager{}
		mtm.On("GetTarget", mock.Anything).Return(localTarget, nil)

		response := target.TestTargetResponse{
			ConnectionStatus: target.TargetTestConnection{ConnectionStatus: apapproto.ConnectionStatus_CONNECTION_STATUS_OK, Error: nil},
		}
		mdt := &mockTargetTester{}
		mdt.On("TestTarget", mock.Anything, mock.Anything, localTarget).Return(response, nil)

		cmd := newTargetTestCmd(cc, mtm, mdt, loginService)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"localTarget"})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()

		assert.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), "Target Connection - Success")
		assert.Contains(t, cmdBuf.String(), "Test completed with 0 errors.")
	})

	t.Run("android target fails before connection when Android targets are disabled", func(t *testing.T) {
		wasEnabled := viper.GetBool(enableAndroidTargetsConfigKey)
		viper.Set(enableAndroidTargetsConfigKey, false)
		defer viper.Set(enableAndroidTargetsConfigKey, wasEnabled)

		androidTarget := &engine_target.AndroidTarget{SerialNumber: "device-123"}
		loginService := targetloginmocks.NewMockLoginService(t)
		mtm := &target.MockTargetManager{}
		mtm.On("GetTarget", "androidTarget").Return(androidTarget, nil)
		cc := &mocks.MockAutostartClientConnector{}
		mdt := &mockTargetTester{}

		cmd := newTargetTestCmd(cc, mtm, mdt, loginService)
		cmd.SetArgs([]string{"androidTarget"})

		err := cmd.Execute()

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		require.True(t, ok)
		assert.Equal(t, message.CommonUnsupportedTargetType, msgErr.Code())
		assert.Equal(t, "android", msgErr.Metadata()["targetType"])
		cc.AssertNotCalled(t, "ApapClient", mock.Anything)
		mdt.AssertNotCalled(t, "TestTarget", mock.Anything, mock.Anything)
		mtm.AssertExpectations(t)
	})

	t.Run("valid target with persistent flag", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, functionalTarget, nil)
		setDefaultLookupFunc()
		mtm := createMockTargetManager()
		mdt := createMockTargetTester()

		cmd := newTargetTestCmd(cc, mtm, mdt, loginService)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"--target", "functionalTarget"})
		err := cmd.Execute()
		require.NoError(t, err)
	})

	t.Run("valid json target with persistent flag succeeds", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, functionalTarget, nil)
		setDefaultLookupFunc()
		mtm := createMockTargetManager()
		mdt := createMockTargetTester()

		cmd := newTargetTestCmd(cc, mtm, mdt, loginService)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"--target", "{\"type\": \"ssh\", \"value\":{\"jumps\":[{\"host\": \"A\", \"port\": 22}]}}"})
		err := cmd.Execute()
		require.NoError(t, err)
	})

	t.Run("returns error when when client connector fails", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		setDefaultLookupFunc()
		mtm := createMockTargetManager()
		mdt := createMockTargetTester()

		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(nil, rektError)

		cmd := newTargetTestCmd(cc, mtm, mdt, loginService)
		cmd.SetArgs([]string{"functionalTarget"})

		err := cmd.Execute()
		assert.Equal(t, rektError, err)
	})

	t.Run("JSON output includes full error chain", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, functionalTarget, nil)
		setDefaultLookupFunc()
		mtm := createMockTargetManager()

		err1 := errors.New("test error")
		err2 := message.New(message.EngineConductorSshMissingKeyForJump).WithCause(err1).WithMetadata(map[string]string{"jumpNode": "madeUp"})

		customTargetResponse := target.TestTargetResponse{
			ConnectionStatus: target.TargetTestConnection{ConnectionStatus: 1, Error: err2},
		}
		mdt := &mockTargetTester{}
		mdt.Mock.On("TestTarget", mock.Anything, mock.Anything, functionalTarget).Return(customTargetResponse, nil)

		cmd := newTargetTestCmd(cc, mtm, mdt, loginService)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"functionalTarget", "--json"})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()

		assert.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), "\"connection\":{\"status\":1,\"error\":{\"message_code\":\"engine.conductor.ssh.MISSING_KEY_FOR_JUMP\",\"severity\":\"Error\"")
		assert.Contains(t, cmdBuf.String(), "\"metadata\":{\"jumpNode\":\"madeUp\"")
		assert.Contains(t, cmdBuf.String(), "\"children\":[{\"message_code\":\"\",\"severity\":\"Error\",\"message\":\"test error\"")
		assert.True(t, utils.IsValidJSON(cmdBuf.String()))
	})

	t.Run("GetTarget is called with the specific target name from flag", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, functionalTarget, nil)
		mtm := &target.MockTargetManager{}
		mtm.On("GetTarget", "valid_target").Return(functionalTarget, nil)

		mdt := createMockTargetTester()

		cmd := newTargetTestCmd(cc, mtm, mdt, loginService)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"--target", "valid_target"})

		err := cmd.Execute()

		require.NoError(t, err)
		mtm.AssertCalled(t, "GetTarget", "valid_target")
	})

}
