// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"path"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
)

// componentCompressionConflict rejects different compression settings unless both
// logical and stored destinations are provably separate. Sources are not compared.
// Following are examples where the first destination is compressed and the second is uncompressed:
//
//   - Allowed: entity/boo.csv and entity/foo.csv (different files).
//   - Allowed: entity/*.csv and entity/*.parquet (different extensions).
//   - Allowed: entity/disassembly* and entity/parquet/** (separate path segments).
//   - Rejected: entity/* and entity/*.parquet (logical overlap).
//   - Rejected: entity/foo.csv and entity/foo.csv.zst (same stored file).
//   - Rejected: entity/foo and entity/foo.zst/bar.csv (stored file/directory conflict).
//   - Rejected: entity/foo and entity/foo/bar.csv (logical ancestor/descendant).
//   - Rejected: entity/**/*.csv and entity/**/*.parquet (separation cannot be
//     proven before **; the policy deliberately avoids recursive glob analysis).
//
// Same-compression overlaps retain their existing behavior.
func componentCompressionConflict(a, b cdf.ManifestEntry) bool {
	return a.Compressed != b.Compressed &&
		(!destinationsProvablyDisjoint(a.Path, b.Path) || !destinationsProvablyDisjoint(a.StoragePath(), b.StoragePath()))
}

// destinationsProvablyDisjoint compares corresponding path segments, stopping at
// recursive or complex patterns. It deliberately rejects cases it cannot prove safe.
// Comparisons ignore case on every OS to protect runs on case-insensitive filesystems.
func destinationsProvablyDisjoint(a, b string) bool {
	left, right := strings.Split(strings.ToLower(a), "/"), strings.Split(strings.ToLower(b), "/")
	for i := 0; i < len(left) && i < len(right); i++ {
		if strings.ContainsAny(left[i]+right[i], "?[{") {
			return false
		}
		// ** can change segment alignment; multiple stars need a more complex analysis.
		if strings.Count(left[i], "*") > 1 || strings.Count(right[i], "*") > 1 {
			return false
		}
		if destinationSegmentsDisjoint(left[i], right[i]) {
			return true
		}
	}
	// Matching prefixes may require the same destination to be both a file and a directory.
	return false
}

// destinationSegmentsDisjoint handles literals and patterns with at most one *.
func destinationSegmentsDisjoint(a, b string) bool {
	if !strings.Contains(a, "*") {
		matches, _ := path.Match(b, a)
		return !matches
	}
	if !strings.Contains(b, "*") {
		matches, _ := path.Match(a, b)
		return !matches
	}
	prefixA, suffixA, _ := strings.Cut(a, "*")
	prefixB, suffixB, _ := strings.Cut(b, "*")
	prefixesDisjoint := !strings.HasPrefix(prefixA, prefixB) && !strings.HasPrefix(prefixB, prefixA)
	suffixesDisjoint := !strings.HasSuffix(suffixA, suffixB) && !strings.HasSuffix(suffixB, suffixA)
	return prefixesDisjoint || suffixesDisjoint
}
