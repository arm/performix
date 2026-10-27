// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package main

import "golang.org/x/sys/unix"

func currentAffinity() []int {
	var mask unix.CPUSet
	if err := unix.SchedGetaffinity(0, &mask); err != nil {
		return nil
	}
	var cpus []int
	for cpu := 0; cpu < 1024; cpu++ {
		if mask.IsSet(cpu) {
			cpus = append(cpus, cpu)
		}
	}
	return cpus
}
