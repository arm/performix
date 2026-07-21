// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package reference

import (
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

// GenerateSlugIdentifierFromTitle computes a stable dotted identifier from a human label. Prefer manually curated
// identifiers where available, but this function can be used to generate them automatically for cases where none
// is provided.
// Rules:
//   - lower-case, non-alnum -> '.'
//   - ':' also becomes a '.'
//   - collapse multiple dots, trim edges
func GenerateSlugIdentifierFromTitle(label string) render.SlugIdentifier {
	lab := strings.ToLower(label)
	lab = strings.ReplaceAll(lab, ":", ".")
	var b strings.Builder
	for _, r := range lab {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('.')
		}
	}
	s := strings.Trim(b.String(), ".")
	parts := make([]string, 0, len(s))
	for _, p := range strings.Split(s, ".") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return render.SlugIdentifier(strings.Join(parts, "."))
}
