// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type Importer interface {
	ImportRun(client apapproto.ApapClient, request *apapproto.RunImportRequest) (*apapproto.RunImportResponse, error)
}

type ImportService struct{}

func (s ImportService) ImportRun(client apapproto.ApapClient, request *apapproto.RunImportRequest) (*apapproto.RunImportResponse, error) {
	return client.ImportRun(context.Background(), request)
}
