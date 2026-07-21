// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cmdsync

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

var ErrorRecipeCancel error = errors.New("recipe run cancelled")

// CommandStateType represents the command type using bit flags
type CommandStateType uint32

const (
	CommandStop   CommandStateType = 1 << iota // 1
	CommandCancel                              // 2
)

type cancelErrorInfo struct {
	err error
}

// CommandState stores the atomic command state
type CommandState struct {
	RecipeCommand atomic.Uint32
	cancelErr     atomic.Pointer[cancelErrorInfo]
}

// Read reads the state atomically
func (s *CommandState) Read() CommandStateType {
	val := s.RecipeCommand.Load()
	return CommandStateType(val)
}

func (s *CommandState) Reset() {
	s.RecipeCommand.Store(0)
	s.cancelErr.Store(nil)
}

// Set writes the provided command state atomically.
func (s *CommandState) Set(flag CommandStateType) {
	s.RecipeCommand.Store(uint32(flag))
}

// SetCancelError records the reason for a cancellation.
func (s *CommandState) SetCancelError(err error) {
	if err == nil {
		s.cancelErr.Store(nil)
		return
	}
	s.cancelErr.Store(&cancelErrorInfo{err: err})
}

// CancelError returns the recorded cancellation error, if any.
func (s *CommandState) CancelError() error {
	val := s.cancelErr.Load()
	if val == nil {
		return nil
	}
	return val.err
}

// CommandStateMap is a concurrent-safe map of CommandState instances
type CommandStateMap interface {
	CreateCommandState(id run.RunID) *CommandState
	Remove(id run.RunID) error
	Write(id run.RunID, flag CommandStateType) error
}

type commandStateMap struct {
	Bus sync.Map
}

// CreateCommandState adds a new CommandState to the map
func (b *commandStateMap) CreateCommandState(id run.RunID) *CommandState {
	state := &CommandState{}
	state.RecipeCommand.Store(0)
	b.Bus.Store(id, state)
	return state
}

// Remove removes a CommandState from the map
// Returns an error if the id doesn't exist
func (b *commandStateMap) Remove(id run.RunID) error {
	err := b.Write(id, 0)
	if err == nil {
		b.Bus.Delete(id)
	}
	return err
}

// Write writes to a specific CommandState in the map
// Returns an error if the id doesn't exist
func (b *commandStateMap) Write(id run.RunID, flag CommandStateType) error {
	value, ok := b.Bus.Load(id)
	if !ok {
		return fmt.Errorf("recipe command state for run %v does not exist", id)
	}
	state := value.(*CommandState)
	state.RecipeCommand.Store(uint32(flag))
	return nil
}

// NewCommandStateMap initializes a new CommandStateMap
func NewCommandStateMap() CommandStateMap {
	return &commandStateMap{}
}
