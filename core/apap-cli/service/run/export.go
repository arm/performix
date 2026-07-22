// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type Exporter interface {
	ExportRun(client apapproto.ApapClient, request *apapproto.RunExportRequest) (*apapproto.RunExportResponse, error)
}

type ExportService struct{}

func (s ExportService) ExportRun(client apapproto.ApapClient, request *apapproto.RunExportRequest) (*apapproto.RunExportResponse, error) {
	return client.ExportRun(context.Background(), request)
}
