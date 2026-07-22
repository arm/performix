// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

// Info defines interfaces to display individual runs
type Info interface {
	ListRun(client apapproto.ApapClient, req *apapproto.GetRunDescriptionRequest) (clijson.CLIRunDescription, error)
}

type InfoService struct{}

func (s InfoService) ListRun(client apapproto.ApapClient, req *apapproto.GetRunDescriptionRequest) (clijson.CLIRunDescription, error) {
	desc, err := client.GetRunDescription(context.Background(), req)
	if err != nil {
		return clijson.CLIRunDescription{}, err
	}

	return clijson.CLIRunDescriptionFromProto(req.Id.Value, desc)
}
