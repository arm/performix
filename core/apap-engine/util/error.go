// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"errors"
	"fmt"
	"strings"
)

// DisplayErrorStringSlice formats a slice of strings for display in an error message's metadata.
// If the slice is nil or empty, it returns "none".
func DisplayErrorStringSlice(s []string) string {
	if len(s) == 0 {
		return "none"
	}
	return fmt.Sprintf("`%v`", strings.Join(s, "`, `"))
}

// JoinErrors joins two errors, but only if both errors are non-nil. If either error is nil,
// the other is returned.
func JoinErrors(errA error, errB error) error {
	if errA == nil {
		return errB
	}
	if errB == nil {
		return errA
	}
	return errors.Join(errA, errB)
}
