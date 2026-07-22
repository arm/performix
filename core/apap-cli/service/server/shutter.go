// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/daemon"
	"github.com/Arm-Debug/apap-cli/apap-engine/pidfiles"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type shutter struct{}

func (s shutter) Shutdown(client apapproto.ApapClient) error {
	_, err := client.Shutdown(context.Background(), &emptypb.Empty{})
	return err
}

func (s shutter) Kill(host string, port int) error {
	pidfile, err := pidfiles.ConstructPidFilePath(host, port)
	if err != nil {
		return err
	}
	return daemon.NewDaemon().Kill(pidfile)
}

func NewShutter() shutter {
	return shutter{}
}
