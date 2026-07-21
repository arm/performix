// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

type foregroundRunner struct{}

func (s foregroundRunner) Run(config grpcserver.GrpcServerConfig) error {
	server := grpcserver.NewGrpcServer(config)
	err := server.RunBlocking()
	if err != nil {
		return message.New(message.CliServiceServerCannotStartNewServer).WithCause(err)
	}
	return nil
}

func NewForegroundRunner() foregroundRunner {
	return foregroundRunner{}
}
