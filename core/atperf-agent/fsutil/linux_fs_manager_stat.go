// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package fsutil

import "syscall"

func statTimes(stat *syscall.Stat_t) (int64, int64) {
	return stat.Atim.Sec*1000 + stat.Atim.Nsec/1e6,
		stat.Ctim.Sec*1000 + stat.Ctim.Nsec/1e6
}
