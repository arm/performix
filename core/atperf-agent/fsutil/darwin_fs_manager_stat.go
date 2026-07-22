// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package fsutil

import "syscall"

func statTimes(stat *syscall.Stat_t) (int64, int64) {
	return stat.Atimespec.Sec*1000 + stat.Atimespec.Nsec/1e6,
		stat.Ctimespec.Sec*1000 + stat.Ctimespec.Nsec/1e6
}
