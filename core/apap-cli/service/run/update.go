// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type Updater interface {
	UpdateRuns(client apapproto.ApapClient, request *apapproto.UpdateRunsRequest) (*apapproto.UpdateRunsResponse, error)
}

type UpdateService struct{}

func (s UpdateService) UpdateRuns(client apapproto.ApapClient, request *apapproto.UpdateRunsRequest) (*apapproto.UpdateRunsResponse, error) {
	return client.UpdateRuns(context.Background(), request)
}
