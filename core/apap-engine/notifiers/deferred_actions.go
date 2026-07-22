// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package notifiers

import "sync"

// DeferredActions is a thread safe collection of cleanups to be called at the end of a recipe run
type DeferredActions struct {
	cleanupFuncs []func()
	lock         sync.Mutex
}

// Adds a deferred action to the list
func (c *DeferredActions) AddAction(f func()) {
	c.lock.Lock()
	c.cleanupFuncs = append(c.cleanupFuncs, f)
	c.lock.Unlock()
}

// Invokes all actions and clears the list
func (c *DeferredActions) InvokeAll() {
	c.lock.Lock()
	cleanups := c.cleanupFuncs
	c.cleanupFuncs = nil
	c.lock.Unlock()
	for _, f := range cleanups {
		f()
	}
}
