// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
//
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package process

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupControllerIsUnsupported(t *testing.T) {
	controller, err := NewGroupController(GroupControllerConfig{})
	require.Nil(t, controller)
	require.EqualError(t, err, "GroupController is not supported on Darwin")
}
