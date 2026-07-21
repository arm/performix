// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type VersionService struct{}

func (s VersionService) GetVersion(client apapproto.ApapClient) (string, error) {
	res, err := client.GetVersion(context.Background(), &emptypb.Empty{})
	if err != nil {
		return "", err
	}

	return res.GetVersion(), nil
}
