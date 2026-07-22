// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"path/filepath"

	"github.com/spf13/afero"
)

type FileHandler struct {
	HostFS afero.Fs
}

// ReadHostFile reads the file at the path specified on the host machine.
func (f *FileHandler) ReadHostFile(filePath string) ([]byte, error) {
	return afero.ReadFile(f.HostFS, filepath.FromSlash(filePath))
}
