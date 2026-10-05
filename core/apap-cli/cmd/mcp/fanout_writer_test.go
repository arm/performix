// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
}

func TestFanoutWriter(t *testing.T) {
	payload := []byte("shutdown record")

	t.Run("writes to every destination", func(t *testing.T) {
		var first, second bytes.Buffer

		n, err := fanoutWriter{&first, &second}.Write(payload)

		require.NoError(t, err)
		assert.Equal(t, len(payload), n)
		assert.Equal(t, payload, first.Bytes())
		assert.Equal(t, payload, second.Bytes())
	})

	t.Run("continues after the first destination fails", func(t *testing.T) {
		expectedErr := errors.New("stderr closed")
		var second bytes.Buffer

		n, err := fanoutWriter{writerFunc(func([]byte) (int, error) {
			return 0, expectedErr
		}), &second}.Write(payload)

		assert.ErrorIs(t, err, expectedErr)
		assert.Zero(t, n)
		assert.Equal(t, payload, second.Bytes())
	})

	t.Run("continues after the second destination fails", func(t *testing.T) {
		expectedErr := errors.New("log file closed")
		var first bytes.Buffer

		n, err := fanoutWriter{&first, writerFunc(func([]byte) (int, error) {
			return 0, expectedErr
		})}.Write(payload)

		assert.ErrorIs(t, err, expectedErr)
		assert.Zero(t, n)
		assert.Equal(t, payload, first.Bytes())
	})

	t.Run("joins errors from every destination", func(t *testing.T) {
		firstErr := errors.New("stderr closed")
		secondErr := errors.New("log file closed")

		n, err := fanoutWriter{
			writerFunc(func([]byte) (int, error) { return 0, firstErr }),
			writerFunc(func([]byte) (int, error) { return 0, secondErr }),
		}.Write(payload)

		assert.ErrorIs(t, err, firstErr)
		assert.ErrorIs(t, err, secondErr)
		assert.Zero(t, n)
	})

	t.Run("reports a short write and continues", func(t *testing.T) {
		var second bytes.Buffer

		n, err := fanoutWriter{writerFunc(func(p []byte) (int, error) {
			return len(p) - 1, nil
		}), &second}.Write(payload)

		assert.ErrorIs(t, err, io.ErrShortWrite)
		assert.Equal(t, len(payload)-1, n)
		assert.Equal(t, payload, second.Bytes())
	})
}
