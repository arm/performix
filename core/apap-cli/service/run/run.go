// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type RunService struct{}

func (s RunService) ResolveRun(client apapproto.ApapClient, name string) (*apapproto.RunId, error) {
	return &apapproto.RunId{}, nil
}
