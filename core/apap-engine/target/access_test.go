// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func doneChannelAfter(tm time.Duration) chan struct{} {
	doneChannel := make(chan struct{})
	go func(done chan struct{}) {
		time.Sleep(tm)
		done <- struct{}{}
	}(doneChannel)
	return doneChannel
}

func TestAccess(t *testing.T) {

	t.Run("test concurrent locks and unlocks for race conditions", func(t *testing.T) {
		ta := TargetAccess{}

		wg := sync.WaitGroup{}
		iters := 200

		targets := []*SSHTarget{
			{Jumps: []SSHHostConfig{{Host: "123.4.3.1", Port: 22}}},
			{Jumps: []SSHHostConfig{{Host: "123.4.3.3", Port: 22}, {Host: "123.4.3.5", Port: 22}}},
			{Jumps: []SSHHostConfig{{Host: "123.4.3.1", Port: 52}}},
			{Jumps: []SSHHostConfig{{Host: "123.4.3.1", Port: 213}}},
		}
		wg.Add(iters * len(targets))

		for _, t := range targets {
			for i := 0; i < iters; i++ {
				go func() {
					lock := ta.LockTarget(t, "any")
					lock.Unlock()
					wg.Done()
				}()
			}
		}
		wg.Wait()

		for _, target := range targets {
			assert.Equal(t, "", ta.GetTargetActiveCommand(target))
		}
	})

	t.Run("Verify active command set", func(t *testing.T) {
		ta := TargetAccess{}
		ti := &SSHTarget{Jumps: []SSHHostConfig{{Host: "123.4.3.3", Port: 22}}}

		lock := ta.LockTarget(ti, "firstCommand")
		assert.NotNil(t, lock)
		assert.Equal(t, ta.GetTargetActiveCommand(ti), "firstCommand")

		lock.Unlock()
		assert.Equal(t, ta.GetTargetActiveCommand(ti), "")
	})

	t.Run("different targets lock independently", func(t *testing.T) {
		ta := TargetAccess{}
		tiA := &SSHTarget{Jumps: []SSHHostConfig{{Host: "123.4.3.3", Port: 22}}}
		tiB := &SSHTarget{Jumps: []SSHHostConfig{{Host: "123.4.3.9", Port: 22}}}

		lockA := ta.LockTarget(tiA, "testLockA")
		assert.NotNil(t, lockA)
		lockB := ta.LockWithCancellation(tiB, "testLockB", doneChannelAfter(time.Millisecond*1))
		assert.NotNil(t, lockB)

		lockAExpectedToFail := ta.LockWithCancellation(tiA, "testLockA", doneChannelAfter(time.Millisecond*1))
		assert.Nil(t, lockAExpectedToFail)
	})

	t.Run("lock succeeds when no active lock", func(t *testing.T) {
		ta := TargetAccess{}
		ti := &SSHTarget{Jumps: []SSHHostConfig{{Host: "123.4.3.3", Port: 22}}}

		locked := ta.LockWithCancellation(ti, "testLockA", doneChannelAfter(time.Millisecond*1))
		assert.NotNil(t, locked)
	})

	t.Run("try lock fails when active lock", func(t *testing.T) {
		ta := TargetAccess{}
		ti := &SSHTarget{Jumps: []SSHHostConfig{{Host: "123.4.3.3", Port: 22}}}

		ta.LockTarget(ti, "test")

		lockedExpectToCancel := ta.LockWithCancellation(ti, "test", doneChannelAfter(time.Millisecond*1))
		assert.Nil(t, lockedExpectToCancel)
	})
}
