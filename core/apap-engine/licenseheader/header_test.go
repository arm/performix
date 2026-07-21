// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package licenseheader

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

func TestWriteGoWritesHeaderAsGoComments(t *testing.T) {
	repoRoot := t.TempDir()
	header := repoCopyrightLicenseHeader(t)
	require.NoError(t, os.WriteFile(
		filepath.Join(repoRoot, fileName),
		header,
		perms.LocalFilePerm,
	))

	var out bytes.Buffer
	err := WriteGo(&out, repoRoot)

	require.NoError(t, err)
	assert.Equal(t, asGoCommentHeader(header), out.String())
}

func TestWriteGoReturnsErrorWhenHeaderFileIsMissing(t *testing.T) {
	var out bytes.Buffer
	err := WriteGo(&out, t.TempDir())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read")
	assert.Empty(t, out.String())
}

func TestWriteGoReturnsErrorWhenHeaderFileIsEmpty(t *testing.T) {
	repoRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, fileName), []byte("\n\t\n"), perms.LocalFilePerm))

	var out bytes.Buffer
	err := WriteGo(&out, repoRoot)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not contain an SPDX header snippet")
	assert.Empty(t, out.String())
}

func TestWriteGoReturnsErrorWhenWriterFails(t *testing.T) {
	repoRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, fileName), repoCopyrightLicenseHeader(t), perms.LocalFilePerm))

	err := WriteGo(failingWriter{}, repoRoot)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write SPDX header")
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func repoCopyrightLicenseHeader(t *testing.T) []byte {
	t.Helper()

	_, sourceFile, _, ok := runtime.Caller(0)
	require.True(t, ok)

	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", ".."))
	header, err := os.ReadFile(filepath.Join(repoRoot, fileName))
	require.NoError(t, err)
	return header
}

func asGoCommentHeader(header []byte) string {
	var out strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(string(header)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out.WriteString("// ")
		out.WriteString(line)
		out.WriteString("\n")
	}
	out.WriteString("\n")
	return out.String()
}
