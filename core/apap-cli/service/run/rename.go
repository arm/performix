// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type Renamer interface {
	RenameRun(client apapproto.ApapClient, id *apapproto.RunId, newName string) (*apapproto.RunRenameResponse, error)
}

type RenameService struct{}

func (s RenameService) RenameRun(client apapproto.ApapClient, id *apapproto.RunId, newName string) (*apapproto.RunRenameResponse, error) {
	return client.RenameRun(context.Background(), &apapproto.RunRenameRequest{RunId: id, NewName: newName})
}
