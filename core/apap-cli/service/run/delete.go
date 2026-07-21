// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

// Deleter defines interfaces to delete a run
type Deleter interface {
	DeleteRuns(client apapproto.ApapClient, request *apapproto.DeleteRunsRequest) (*apapproto.DeleteRunsResponse, error)
}

type DeleteService struct{}

func (s DeleteService) DeleteRuns(client apapproto.ApapClient, request *apapproto.DeleteRunsRequest) (*apapproto.DeleteRunsResponse, error) {
	return client.DeleteRuns(context.Background(), request)
}
