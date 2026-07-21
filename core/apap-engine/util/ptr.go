// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

// Ptr allows to take a reference to a temporary value in a shorthand way - util.Ptr("x") will get a pointer to the string "x"
func Ptr[T any](v T) *T {
	return &v
}
