// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

func TestLoadSymbolsFile(t *testing.T) {

	t.Run("LoadSymbolsFile doesn't error when source data is missing", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := os.WriteFile(tmpDir+"/symbols.json", []byte(`
		[
			{
				"id": 126,
				"name": "compute_symbolic_block_difference_1plane(astcenc_config const&, block_size_descriptor const&, symbolic_compressed_block const&, image_block const&)",
				"image_name": "astcenc-neon",
				"image_id": 123,
				"source_line_info": null
			},

			{
				"id": 124,
				"name": "encode_ise(quant_method, unsigned int, unsigned char const*, unsigned char*, unsigned int)",
				"image_name": "astcenc-neon",
				"image_id": 123,
				"source_line_info": {
						source_file_id: 32041,
						source_file_path: "my/nontrivialpath/blah.cpp",
						first_source_line: x,
						last_source_line: y
				}
			},
	  ]
		`), perms.LocalFilePerm)
		require.NoError(t, err)

	})
}
