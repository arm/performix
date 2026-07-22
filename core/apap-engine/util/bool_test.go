// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCountTrue(t *testing.T) {
	assert.Equal(t, 0, CountTrue())
	assert.Equal(t, 0, CountTrue(false, false))
	assert.Equal(t, 1, CountTrue(false, true, false))
	assert.Equal(t, 3, CountTrue(true, true, false, true))
}
