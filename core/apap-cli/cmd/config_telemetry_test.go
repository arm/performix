// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/test"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/api/mocks"
)

func TestTelemetryCommand(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		for _, setting := range []struct {
			name    string
			args    []string
			enabled bool
			set     bool
		}{
			{name: "read enabled", args: []string{"--telemetry"}, enabled: true},
			{name: "read disabled", args: []string{"--telemetry"}, enabled: false},
			{name: "enable", args: []string{"--telemetry=true"}, enabled: true, set: true},
			{name: "disable", args: []string{"--telemetry=false"}, enabled: false, set: true},
		} {
			t.Run(fmt.Sprintf("%s/json=%t", setting.name, jsonOutput), func(t *testing.T) {
				test.SetViperJSON(t, jsonOutput)
				engine := apapprotomocks.NewApapClient(t)
				cc := &mocks.MockAutostartClientConnector{}
				cc.SetApapClient(engine, nil)
				if !setting.set {
					engine.On("GetProductTelemetryPreference", mock.Anything, &emptypb.Empty{}).
						Return(&apapproto.ProductTelemetryPreference{Enabled: setting.enabled}, nil).Once()
				} else {
					engine.On("SetProductTelemetryPreference", mock.Anything, &apapproto.ProductTelemetryPreference{Enabled: setting.enabled}).
						Return(&emptypb.Empty{}, nil).Once()
				}
				var output bytes.Buffer
				cmd := NewConfigGetCmd(cc)
				if setting.set {
					cmd = NewConfigSetCmd(cc)
				}
				cmd.SetOut(&output)
				cmd.SetArgs(setting.args)
				require.NoError(t, cmd.Execute())
				if jsonOutput {
					var response clijson.CliJSONResponse[bool]
					require.NoError(t, json.Unmarshal(output.Bytes(), &response))
					assert.Equal(t, "0", response.Code)
					assert.Equal(t, setting.enabled, response.Data)
				} else {
					assert.Equal(t, fmt.Sprintf("%t\n", setting.enabled), output.String())
				}
			})
		}
	}
}

func TestTelemetryCommandRejectsInvalidArgumentsBeforeConnecting(t *testing.T) {
	for _, invalid := range []struct {
		args []string
		set  bool
	}{
		{},
		{set: true},
		{args: []string{"--telemetry=maybe"}, set: true},
		{args: []string{"--telemetry", "extra"}},
		{args: []string{"--telemetry=false", "extra"}, set: true},
	} {
		cc := &mocks.MockAutostartClientConnector{}
		cmd := NewConfigGetCmd(cc)
		if invalid.set {
			cmd = NewConfigSetCmd(cc)
		}
		cmd.SetArgs(invalid.args)
		require.Error(t, cmd.Execute())
		cc.AssertNotCalled(t, "ApapClient", mock.Anything)
	}
}

func TestTelemetryCommandReportsErrors(t *testing.T) {
	for _, operation := range []string{"connect", "read", "save"} {
		t.Run(operation, func(t *testing.T) {
			engine := apapprotomocks.NewApapClient(t)
			cc := &mocks.MockAutostartClientConnector{}
			failure := errors.New("telemetry operation failed")
			if operation == "connect" {
				cc.SetApapClient(nil, failure)
			} else {
				cc.SetApapClient(engine, nil)
			}
			cmd := NewConfigGetCmd(cc)
			if operation == "save" {
				cmd = NewConfigSetCmd(cc)
				cmd.SetArgs([]string{"--telemetry=false"})
				engine.On("SetProductTelemetryPreference", mock.Anything, mock.Anything).Return(nil, failure).Once()
			} else {
				cmd.SetArgs([]string{"--telemetry"})
				if operation == "read" {
					engine.On("GetProductTelemetryPreference", mock.Anything, mock.Anything).Return(nil, failure).Once()
				}
			}
			require.ErrorContains(t, cmd.Execute(), failure.Error())
		})
	}
}
