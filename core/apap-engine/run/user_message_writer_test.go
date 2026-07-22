// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	cdfmocks "github.com/Arm-Debug/apap-cli/apap-engine/cdf/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

func TestConcreteUserMessageWriter(t *testing.T) {
	tempDir := t.TempDir()
	expectedComponent := cdf.ComponentType{Name: cdf.TypeLogJSON, SchemaVersion: "0.1"}
	componentPath := filepath.Join("user_messages", "user_messages.json")
	absolutePath := filepath.Join(tempDir, componentPath)

	store := &cdfmocks.MockComponentStore{}
	store.On("StoreComponent", componentPath, expectedComponent).Run(func(mock.Arguments) {
		require.NoError(t, os.MkdirAll(filepath.Dir(absolutePath), perms.LocalDirPerm))
	}).Return(absolutePath, nil).Once()

	writer := &ConcreteUserMessageWriter{}

	closer, err := writer.Open(store)
	require.NoError(t, err)
	require.NotNil(t, closer)

	writer.Write("warn", "my user message")
	require.NoError(t, closer.Close())

	store.AssertExpectations(t)

	content, err := os.ReadFile(absolutePath)
	require.NoError(t, err)
	assert.Contains(t, string(content), `"severity":"warning"`)
	assert.Contains(t, string(content), `"message":"my user message"`)
}

func TestConcreteUserMessageWriter_OpenStoreError(t *testing.T) {
	expectedComponent := cdf.ComponentType{Name: cdf.TypeLogJSON, SchemaVersion: "0.1"}
	componentPath := filepath.Join("user_messages", "user_messages.json")
	myError := errors.New("store error")

	store := &cdfmocks.MockComponentStore{}
	store.On("StoreComponent", componentPath, expectedComponent).Return("", myError).Once()

	writer := &ConcreteUserMessageWriter{}
	closer, err := writer.Open(store)
	require.ErrorIs(t, err, message.New(message.EngineRunUserMessageOpenFailed).WithCause(myError))
	assert.Nil(t, closer)
	store.AssertExpectations(t)
}
