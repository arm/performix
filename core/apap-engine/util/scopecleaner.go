// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"sync"
)

type DoNotCopy [0]sync.Mutex

// ScopeCleaner is intended for situations where you are creating multiple resources on the stack, and want some way to
// clean them all up in the case of failure, but cancel that cleanup in the case of success
//
// For example:
//
//	func Construct() (Resource, Resource, error) {
//		res1, err := CreateResource()
//		if err {
//		   return nil, nil, err
//		}
//
//		res2, err := CreateResource()
//		if err {
//			res1.Close() // annoying: I have to close this!
//			return nil, nil, err
//		}
//
//		return res1, res2, nil
//	}
//
// This becomes very error prone with more items. We should normally use defer to handle closing things at scope exit,
// but in the case of success we need to cancel the deferred cleanup and return normally. ScopeCleaner helps with this.
//
// For example:
//
//	func Construct() (Resource, Resource, error) {
//		cleaner := ScopeCleaner{}
//
//		res1, err := CreateResource()
//		if err != nil {
//			return nil, nil, err
//		}
//		defer cleaner.MaybeCleanup(func() { res1.Close() })
//
//		res2, err := CreateResource()
//		if err != nil {
//			return nil, nil, err
//		}
//		defer cleaner.MaybeCleanup(func() { res2.Close() })
//
//		cleaner.CancelCleanup()
//		return res1, res2, nil
//	}
type ScopeCleaner struct {
	DoNotCopy

	Ok bool
}

func (c *ScopeCleaner) MaybeCleanup(f func()) {
	if !c.Ok {
		f()
	}
}

func (c *ScopeCleaner) CancelCleanup() {
	c.Ok = true
}
