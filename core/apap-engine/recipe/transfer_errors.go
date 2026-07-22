// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"fmt"
	"sync"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type transferErrorKey struct {
	remotePath string
	localPath  string
}

func newTransferErrorKey(transfer conductor.FileTransfer) transferErrorKey {
	return transferErrorKey{
		remotePath: transfer.RemotePath,
		localPath:  transfer.LocalPath,
	}
}

func (k transferErrorKey) String() string {
	return fmt.Sprintf("%s -> %s", k.remotePath, k.localPath)
}

type transferErrors struct {
	mu         sync.Mutex
	phase1     map[transferErrorKey]error
	background map[transferErrorKey]error
}

func newTransferErrors() *transferErrors {
	return &transferErrors{
		phase1:     map[transferErrorKey]error{},
		background: map[transferErrorKey]error{},
	}
}

func (e *transferErrors) add(phase transferPhase, transfer conductor.FileTransfer, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	switch phase {
	case backgroundTransferPhase:
		e.background[newTransferErrorKey(transfer)] = err
	default:
		e.phase1[newTransferErrorKey(transfer)] = err
	}
}

func (e *transferErrors) combined() map[transferErrorKey]error {
	e.mu.Lock()
	defer e.mu.Unlock()

	combined := util.CopyMap(e.phase1)
	for key, err := range e.background {
		combined[key] = err
	}
	return combined
}

func (e *transferErrors) phase1Copy() map[transferErrorKey]error {
	e.mu.Lock()
	defer e.mu.Unlock()

	return util.CopyMap(e.phase1)
}

func (e *transferErrors) backgroundCopy() map[transferErrorKey]error {
	e.mu.Lock()
	defer e.mu.Unlock()

	return util.CopyMap(e.background)
}

func transferErrorMetadata(errs map[transferErrorKey]error) map[string]string {
	metadata := make(map[string]string, len(errs))
	for key, err := range errs {
		metadata[key.String()] = err.Error()
	}
	return metadata
}
