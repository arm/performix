// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
)

// FileTransfer represents a file to transfer from remote to local machine.
type FileTransfer struct {
	RemotePath    string
	LocalPath     string
	Exclude       []string
	ComponentType cdf.ComponentType
}
