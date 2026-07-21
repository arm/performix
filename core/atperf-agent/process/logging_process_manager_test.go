// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"errors"
	"os"
	"testing"

	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
)

func TestLoggingProcessManager(t *testing.T) {
	mockPM := new(MockProcessManager)
	logger, hook := test.NewNullLogger()
	loggingPM := NewLoggingProcessManager(mockPM, logger)

	t.Run("InterruptProcess logs and delegates", func(t *testing.T) {
		mockPM.On("InterruptProcess", 123).Return(nil).Once()

		err := loggingPM.InterruptProcess(123)
		assert.NoError(t, err)

		assert.Equal(t, "InterruptProcess succeeded", hook.LastEntry().Message)
		mockPM.AssertExpectations(t)
	})

	t.Run("StartProcess logs and delegates", func(t *testing.T) {
		cmd := &StartProcess{}
		mockProc := &os.Process{}
		mockPM.On("StartProcess", cmd).Return(mockProc, nil).Once()

		proc, err := loggingPM.StartProcess(cmd)
		assert.NoError(t, err)
		assert.Equal(t, mockProc, proc)

		assert.Equal(t, "StartProcess succeeded", hook.LastEntry().Message)
		mockPM.AssertExpectations(t)
	})

	t.Run("ExecCommand logs and delegates", func(t *testing.T) {
		cmd := &LaunchCommand{}
		result := &CommandResult{Rc: 0}
		mockPM.On("ExecCommand", cmd).Return(result, nil).Once()

		res, err := loggingPM.ExecCommand(cmd)
		assert.NoError(t, err)
		assert.Equal(t, result, res)

		assert.Equal(t, "ExecCommand succeeded", hook.LastEntry().Message)
		mockPM.AssertExpectations(t)
	})

	t.Run("KillProcess logs and delegates", func(t *testing.T) {
		mockPM.On("KillProcess", 123).Return(nil).Once()

		err := loggingPM.KillProcess(123)
		assert.NoError(t, err)

		assert.Equal(t, "KillProcess succeeded", hook.LastEntry().Message)
		mockPM.AssertExpectations(t)
	})

	t.Run("StreamStdout logs and delegates", func(t *testing.T) {
		stream := &fakeSender{}
		mockPM.On("StreamStdout", 123, stream).Return(nil).Once()

		err := loggingPM.StreamStdout(123, stream)
		assert.NoError(t, err)

		assert.Equal(t, "StreamStdout succeeded", hook.LastEntry().Message)
		mockPM.AssertExpectations(t)
	})

	t.Run("StreamStderr logs and delegates", func(t *testing.T) {
		stream := &fakeSender{}
		mockPM.On("StreamStderr", 123, stream).Return(nil).Once()

		err := loggingPM.StreamStderr(123, stream)
		assert.NoError(t, err)

		assert.Equal(t, "StreamStderr succeeded", hook.LastEntry().Message)
		mockPM.AssertExpectations(t)
	})

	t.Run("WriteToStdin logs and delegates", func(t *testing.T) {
		data := []byte("test")
		mockPM.On("WriteToStdin", 123, data).Return(nil).Once()

		err := loggingPM.WriteToStdin(123, data)
		assert.NoError(t, err)

		assert.Equal(t, "WriteToStdin succeeded", hook.LastEntry().Message)
		mockPM.AssertExpectations(t)
	})

	t.Run("Logs errors when delegate fails", func(t *testing.T) {
		mockPM.On("KillProcess", 123).Return(errors.New("mock error")).Once()

		err := loggingPM.KillProcess(123)
		assert.Error(t, err)
		assert.Equal(t, "KillProcess failed", hook.LastEntry().Message)
		mockPM.AssertExpectations(t)
	})

	t.Run("Shutdown logs and delegates", func(t *testing.T) {
		mockPM.On("Shutdown", true).Return(nil).Once()

		err := loggingPM.Shutdown(true)
		assert.NoError(t, err)

		assert.Equal(t, "Shutdown succeeded", hook.LastEntry().Message)
		mockPM.AssertExpectations(t)
	})

}
