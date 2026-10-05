// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
)

func TestComponentCompressionConflict(t *testing.T) {
	for _, tc := range []struct {
		name, compressed, plain string
		conflict                bool
	}{
		{"case-only logical difference", "entity/Foo.csv", "entity/foo.csv", true},
		{"case-only physical difference", "entity/Foo.csv", "entity/foo.csv.zst", true},
		{"case-only directory difference", "Entity/foo.csv", "entity/foo.csv.zst", true},
		{"case-insensitive exact and glob", "entity/*.CSV", "entity/foo.csv.zst", true},
		{"case-insensitive glob prefixes and suffixes", "entity/Foo*.CSV", "entity/foo*.csv.ZST", true},
		{"case-insensitive physical ancestor", "Entity/Foo", "entity/foo.ZST/bar.csv", true},
		{"different extensions ignoring case", "Entity/*.CSV", "entity/*.parquet", false},
		{"different exact filenames", "entity/boo.csv", "entity/foo.csv", false},
		{"distinct filename prefixes", "entity/foo*.csv", "entity/bar*.csv", false},
		{"compatible prefix and suffix", "entity/foo*", "entity/*bar", true},
		{"exact filename too short", "entity/a*a", "entity/a", false},
		{"multiple stars are not analysed", "entity/a*b*.csv", "entity/*.parquet", true},
		{"separate trees before recursion", "csv/**", "parquet/**", false},
		{"nested recursion may match zero segments", "entity/foo", "entity/foo/**", true},
		{"separate directories before question mark", "csv/data.csv", "other/?.txt", false},
		{"separate directories before character class", "csv/data.csv", "other/[ab].txt", false},
		{"separate directories before alternatives", "csv/data.csv", "other/{a,b}.txt", false},
		{"complex suffix does not prove depth separation", "entity/foo", "entity/foo/{a,b}", true},
		{"unsupported question mark", "entity/?.csv", "entity/*.parquet", true},
		{"unsupported character class", "entity/[ab].csv", "entity/*.parquet", true},
		{"unsupported alternatives", "entity/{a,b}.csv", "entity/*.parquet", true},
		{"same logical file", "entity/foo.csv", "entity/foo.csv", true},
		{"physical file versus directory", "entity/foo", "entity/foo.zst/bar.csv", true},
		{"logical ancestor versus descendant", "entity/foo", "entity/foo/bar.csv", true},
		{"glob ancestor versus descendant", "entity/foo*", "entity/foobar/child.csv", true},
		{"distinct prefixes at different depths", "entity/foo", "entity/other/bar.csv", false},
		{"same physical file", "entity/foo.csv", "entity/foo.csv.zst", true},
		{"different directories", "dir1/*", "dir2/*", false},
		{"different extensions", "entity/*.csv", "entity/*.parquet", false},
		{"broad glob", "entity/*", "entity/*.parquet", true},
		{"exact logical overlap", "entity/*.csv", "entity/foo.csv", true},
		{"exact physical overlap", "entity/*.csv", "entity/foo.csv.zst", true},
		{"glob physical overlap", "entity/*.csv", "entity/*.csv.zst", true},
		{"disjoint exact and glob", "entity/*.csv", "entity/metadata.json", false},
		{"recursive zero directories", "entity/**/foo.csv", "entity/foo.csv", true},
		{"recursive nested directories", "entity/**/foo.csv", "entity/a/b/foo.csv", true},
		{"star cannot cross directories", "entity/*.csv", "entity/a/foo.csv", false},
		{"recursive separation is not inferred", "entity/**/*.csv", "entity/**/*.parquet", true},
		{"recursive overlap", "entity/**/a/*.csv", "entity/b/**/foo.csv", true},
		{"neoprof disassembly and parquet", "output/disassembly-capture-metrics*", "output/parquet/timeline/**/counter.parquet", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := cdf.ManifestEntry{Path: tc.compressed, Compressed: true}, cdf.ManifestEntry{Path: tc.plain}
			for _, pair := range [][2]cdf.ManifestEntry{{a, b}, {b, a}} {
				got := componentCompressionConflict(pair[0], pair[1])
				require.Equal(t, tc.conflict, got)
			}
		})
	}
}

func TestSameCompressionOverlapsRemainAllowed(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		require.False(t, componentCompressionConflict(cdf.ManifestEntry{Path: "entity/**", Compressed: compressed}, cdf.ManifestEntry{Path: "entity/*.csv", Compressed: compressed}))
	}
}
