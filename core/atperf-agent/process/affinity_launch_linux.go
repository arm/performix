// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package process

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const affinityMaskCPUs = 65536

// startWithAffinity changes only the thread that calls Start. The child
// inherits that thread's mask before it can run. The goroutine exits with the
// thread locked so the runtime terminates it instead of reusing its affinity.
func startWithAffinity(cmd *exec.Cmd, affinity []string) error {
	if len(affinity) == 0 || cmd.Err != nil {
		return cmd.Start()
	}
	mask, err := parseAffinity(affinity)
	if err != nil {
		return err
	}
	result := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		if err := unix.SchedSetaffinityDynamic(0, mask); err != nil {
			result <- fmt.Errorf("set CPU affinity: %w", err)
			return
		}
		result <- cmd.Start()
	}()
	return <-result
}

func parseAffinity(tokens []string) (unix.CPUSetDynamic, error) {
	mask := unix.NewCPUSet(affinityMaskCPUs)
	hasCPU := false
	for _, token := range tokens {
		for _, part := range strings.Split(token, ",") {
			part = strings.TrimSpace(part)
			bounds := strings.Split(part, "-")
			if len(bounds) < 1 || len(bounds) > 2 || bounds[0] == "" {
				return nil, errors.New("invalid CPU affinity specification")
			}
			first, err := strconv.Atoi(bounds[0])
			if err != nil {
				return nil, errors.New("invalid CPU affinity specification")
			}
			last := first
			if len(bounds) == 2 {
				last, err = strconv.Atoi(bounds[1])
				if err != nil {
					return nil, errors.New("invalid CPU affinity specification")
				}
			}
			if first < 0 || last < first || last >= affinityMaskCPUs {
				return nil, errors.New("CPU affinity index is out of range")
			}
			for cpu := first; cpu <= last; cpu++ {
				mask.Set(cpu)
				hasCPU = true
			}
		}
	}
	if !hasCPU {
		return nil, errors.New("empty CPU affinity specification")
	}
	return mask, nil
}
