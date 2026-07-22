// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package privilege

import "fmt"

type UDSAcceptorFactory struct {
}

// NewUDSAcceptorFactory returns a factory for creating UDS acceptors.
func NewUDSAcceptorFactory(socketDir string) *UDSAcceptorFactory {
	return &UDSAcceptorFactory{}
}

func (f *UDSAcceptorFactory) NewAcceptor() (Acceptor, error) {
	return nil, fmt.Errorf("UDSAcceptor is not supported on Windows")
}
