// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package licenseheader

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const fileName = "copyright-license-header.txt"

// WriteGo writes the repository copyright/license snippet as Go comments.
func WriteGo(out io.Writer, repoRoot string) error {
	headerPath := filepath.Join(repoRoot, fileName)
	header, err := os.ReadFile(headerPath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", headerPath, err)
	}

	wroteHeader := false
	for _, line := range strings.Split(strings.TrimSpace(string(header)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, err := fmt.Fprintf(out, "// %s\n", line); err != nil {
			return fmt.Errorf("failed to write SPDX header: %w", err)
		}
		wroteHeader = true
	}
	if !wroteHeader {
		return fmt.Errorf("%s does not contain an SPDX header snippet", headerPath)
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return fmt.Errorf("failed to write SPDX header separator: %w", err)
	}
	return nil
}
