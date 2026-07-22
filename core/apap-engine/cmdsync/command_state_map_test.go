// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cmdsync

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func TestRecipeCommandMap(t *testing.T) {

	t.Run("recipe command state map test one", func(t *testing.T) {
		bus := NewCommandStateMap()

		var wg sync.WaitGroup

		// Create run and initial state
		run1 := run.RunID{Value: "123"}
		state := bus.CreateCommandState(run1)
		assert.Equal(t, CommandStateType(0), state.Read())

		wg.Add(1)
		go func() {
			defer wg.Done()

			// First confirm trying to update non-exsitent run state fails
			noneExistantRun := run.RunID{Value: "456"}
			err := bus.Write(noneExistantRun, CommandStop)
			assert.Error(t, err)
			assert.ErrorContains(t, err, fmt.Sprintf("recipe command state for run %v does not exist", noneExistantRun))

			err = bus.Write(run1, CommandCancel)
			assert.NoError(t, err)
		}()

		// Wait for go routine to finish
		wg.Wait()

		// Assert state is CommandCancel
		assert.Equal(t, CommandCancel, state.Read())
	})

	t.Run("recipe command bus test two", func(t *testing.T) {
		bus := NewCommandStateMap()

		var wg sync.WaitGroup

		// Create run and initial state
		run1 := run.RunID{Value: "123"}
		state := bus.CreateCommandState(run1)
		assert.Equal(t, CommandStateType(0), state.Read())

		// Update state
		_ = bus.Write(run1, CommandStop)
		assert.Equal(t, CommandStop, state.Read())

		// Wipe state now work is completed
		err := bus.Remove(run1)
		assert.NoError(t, err)

		err = bus.Remove(run1)
		assert.Error(t, err)

		wg.Add(1)
		go func() {
			defer wg.Done()

			// Go routine tries to update now removed run1 state
			err := bus.Write(run1, CommandStop)
			assert.Error(t, err)
			assert.ErrorContains(t, err, fmt.Sprintf("recipe command state for run %v does not exist", run1))
		}()

		// Wait for go routine to finish
		wg.Wait()

		err = bus.Remove(run1)
		assert.Error(t, err)
	})

	t.Run("recipe command bus test read to non-existent id reads zero value", func(t *testing.T) {
		bus := NewCommandStateMap()

		var wg sync.WaitGroup

		// Create run and initial state
		run1 := run.RunID{Value: "123"}
		state := bus.CreateCommandState(run1)
		assert.Equal(t, CommandStateType(0), state.Read())

		// Write Stop message for this run
		err := bus.Write(run1, CommandStop)
		assert.NoError(t, err)

		// Go routine runs and reads stop message
		wg.Add(1)
		go func() {
			defer wg.Done()
			val := state.Read()
			assert.Equal(t, CommandStop, val)
		}()
		wg.Wait()

		// Remove the run from the bus - reference to the CommandState "state" still exists and has Stop value
		err = bus.Remove(run1)
		assert.NoError(t, err)

		// Go routine now reads from "stale" state, should read zero value
		wg.Add(1)
		go func() {
			defer wg.Done()

			val := state.Read()
			assert.Equal(t, CommandStateType(0), val)
		}()
		wg.Wait()
	})
}

func TestCommandStateCancelError(t *testing.T) {
	state := &CommandState{}

	t.Run("set and read cancel error", func(t *testing.T) {
		errBoom := errors.New("boom")
		state.SetCancelError(errBoom)
		assert.Equal(t, errBoom, state.CancelError())
	})

	t.Run("set nil clears cancel error", func(t *testing.T) {
		state.SetCancelError(nil)
		assert.NoError(t, state.CancelError())
	})

	t.Run("reset clears cancel error and command state", func(t *testing.T) {
		state.Set(CommandCancel)
		state.SetCancelError(errors.New("cancelled"))

		state.Reset()

		assert.Equal(t, CommandStateType(0), state.Read())
		assert.NoError(t, state.CancelError())
	})
}
