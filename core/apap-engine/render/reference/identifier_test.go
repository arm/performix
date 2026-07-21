// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package reference

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

func TestSlugIdentifier(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Sample Count", "sample.count"},
		{"Cycles: CPU Cycles", "cycles.cpu.cycles"},
		{"L1D Cache MPKI", "l1d.cache.mpki"},
		{" weird :: label  ", "weird.label"},
		{"SVE Operations (Load/Store Inclusive) Percentage", "sve.operations.load.store.inclusive.percentage"},
		{"a---b___c", "a.b.c"},
		{":leading:and:trailing:", "leading.and.trailing"},
	}
	for _, c := range cases {
		assert.Equal(t, render.SlugIdentifier(c.want), GenerateSlugIdentifierFromTitle(c.in), "GenerateSlugIdentifierFromTitle(%q)", c.in)
	}
}
