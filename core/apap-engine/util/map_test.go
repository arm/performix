// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCopyMap(t *testing.T) {
	original := map[string]int{
		"one": 1,
		"two": 2,
	}

	copied := CopyMap(original)
	copied["one"] = 10
	copied["three"] = 3

	assert.Equal(t, map[string]int{
		"one": 1,
		"two": 2,
	}, original)
	assert.Equal(t, map[string]int{
		"one":   10,
		"two":   2,
		"three": 3,
	}, copied)
}
