// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"context"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type Closer interface {
	CloseRender(client apapproto.ApapClient, sessionID string) error
}

type CloseService struct{}

func (s CloseService) CloseRender(client apapproto.ApapClient, sessionID string) error {
	request := apapproto.CloseRenderRequest{
		SessionId: sessionID,
	}

	_, err := client.CloseRender(context.Background(), &request)
	if err != nil {
		return err
	}

	return nil
}
