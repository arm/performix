// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import "time"

type Sleeper interface {
	Sleep(ms int)
}

type ConcreteSleeper struct {
}

func (s ConcreteSleeper) Sleep(ms int) {
	duration := time.Duration(ms)
	time.Sleep(duration * time.Millisecond)
}
