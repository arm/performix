// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package notifiers

import (
	"testing"

	"github.com/stretchr/testify/mock"
)

type mockAction struct{ mock.Mock }

func (m *mockAction) Do() { m.Called() }

func TestDeferredActions_AddAction_InvokeAll(t *testing.T) {
	da := DeferredActions{}

	m1 := new(mockAction)
	m2 := new(mockAction)
	m3 := new(mockAction)

	// Verify actions are invoked once, in the order added.
	mock.InOrder(
		m1.On("Do").Once(),
		m2.On("Do").Once(),
		m3.On("Do").Once(),
	)

	da.AddAction(m1.Do)
	da.AddAction(m2.Do)
	da.AddAction(m3.Do)

	// First invoke: should call each action exactly once in order.
	da.InvokeAll()

	m1.AssertExpectations(t)
	m2.AssertExpectations(t)
	m3.AssertExpectations(t)

	// Second invoke: list should be cleared; should not call anything again.
	da.InvokeAll()

	m1.AssertNumberOfCalls(t, "Do", 1)
	m2.AssertNumberOfCalls(t, "Do", 1)
	m3.AssertNumberOfCalls(t, "Do", 1)
}
