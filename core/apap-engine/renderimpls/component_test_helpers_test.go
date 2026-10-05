// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

func writeCompressedComponent(t *testing.T, relativePath string, contents []byte) cdf.Component {
	t.Helper()

	storedPath := filepath.Join(t.TempDir(), filepath.FromSlash(relativePath)) + cdf.ZstdSuffix
	require.NoError(t, os.MkdirAll(filepath.Dir(storedPath), perms.LocalDirPerm))
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	require.NoError(t, err)
	compressed := encoder.EncodeAll(contents, nil)
	encoder.Close()
	require.NoError(t, os.WriteFile(storedPath, compressed, perms.LocalFilePerm))

	return cdf.Component{
		RelativePath: relativePath,
		AbsolutePath: storedPath,
		Compressed:   true,
	}
}
