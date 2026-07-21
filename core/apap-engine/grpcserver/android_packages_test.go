// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

func TestAndroidPackageListToApapProto(t *testing.T) {
	resp := androidPackageListToApapProto(&targetagentproto.AndroidPackageList{
		Packages: []*targetagentproto.AndroidPackage{
			{
				Name: "com.example.app",
				Activities: []*targetagentproto.AndroidActivity{
					{Name: "com.example.app.MainActivity"},
					{Name: "com.example.app.SettingsActivity"},
				},
			},
		},
	})

	assert.Equal(t, []*apapproto.AndroidPackage{
		{
			Name: "com.example.app",
			Activities: []*apapproto.AndroidActivity{
				{Name: "com.example.app.MainActivity"},
				{Name: "com.example.app.SettingsActivity"},
			},
		},
	}, resp.Packages)
}

func TestListAndroidPackagesRejectsNonAndroidTarget(t *testing.T) {
	server := &ApapServer{}
	req := &apapproto.ListAndroidPackagesRequest{Target: TargetToProto(&target.LocalTarget{})}

	resp, err := server.ListAndroidPackages(context.Background(), req)

	require.Nil(t, resp)
	require.Error(t, err)
	var msg message.Message
	require.True(t, errors.As(err, &msg))
	assert.Equal(t, message.EngineGrpcserverApiApapListAndroidPackagesTargetNotAndroid, msg.Code())
}
