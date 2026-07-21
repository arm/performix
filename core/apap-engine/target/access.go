// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"reflect"
	"sync"

	"go.uber.org/atomic"
)

// TargetLock represents an exclusive lock for a target.
// It ensures that only one operation can hold the lock at a time.
type TargetLock struct {
	target        Target        // The target associated with this lock
	activeCommand atomic.String // The currently active command using this lock
	unlocked      chan struct{} // Channel used to signal when the lock is available
}

// TargetAccess provides exclusive access to targets to prevent concurrent access.
//
// The targets slice only grows with each new TargetInfo encountered; it does not shrink.
// This avoids the complexity of removing targets as overlapping lock requests come and go.
type TargetAccess struct {
	mu      sync.Mutex
	targets []*TargetLock
}

// findOrCreateTargetLock searches for an existing TargetLock for the given target.
// If no existing lock is found, it creates a new one and adds it to the list.
func (ta *TargetAccess) findOrCreateTargetLock(target Target) *TargetLock {
	ta.mu.Lock()
	defer ta.mu.Unlock()
	for _, t := range ta.targets {
		if reflect.DeepEqual(t.target, target) {
			return t
		}
	}
	newLock := &TargetLock{target: target, unlocked: make(chan struct{}, 1)}
	ta.targets = append(ta.targets, newLock)
	newLock.unlocked <- struct{}{} // Initialize the unlocked state
	return newLock
}

// GetTargetActiveCommand retrieves the command that currently holds the target lock.
//
// Note: As soon as this function returns, the active command may already be outdated.
// This method should be used for diagnostic purposes only.
func (ta *TargetAccess) GetTargetActiveCommand(target Target) string {
	ta.mu.Lock()
	defer ta.mu.Unlock()
	for _, t := range ta.targets {
		if reflect.DeepEqual(t.target, target) {
			return t.activeCommand.Load()
		}
	}
	return ""
}

// Unlock releases exclusive access to the target.
// The lock becomes available for other operations.
func (tl *TargetLock) Unlock() {
	tl.activeCommand.Store("")
	tl.unlocked <- struct{}{}
}

// LockWithCancellation attempts to acquire the target lock.
//
// If the lock is granted, a non-nil TargetLock is returned.
// If the cancellation channel is closed or triggered, the request is canceled and nil is returned.
//
// If a lock is granted, the caller must call Unlock on the returned object once finished with the lock.
func (ta *TargetAccess) LockWithCancellation(target Target, operationName string, done <-chan struct{}) *TargetLock {
	tl := ta.findOrCreateTargetLock(target)
	select {
	case <-done:
		return nil // Request was canceled
	case <-tl.unlocked:
		tl.activeCommand.Store(operationName)
		return tl
	}
}

// LockTarget blocks until the target lock is granted.
//
// This request cannot be canceled.
// The caller must call Unlock on the returned object once finished with the lock.
func (ta *TargetAccess) LockTarget(target Target, operationName string) *TargetLock {
	return ta.LockWithCancellation(target, operationName, make(chan struct{}))
}
