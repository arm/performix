// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

type cancelAfterFirstRead struct {
	ctxCancel context.CancelFunc
	read      bool
}

func (r *cancelAfterFirstRead) Read(p []byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	r.ctxCancel()
	copy(p, []byte("hello"))
	return len("hello"), nil
}

func TestCopyWithContext(t *testing.T) {
	t.Run("copies all bytes", func(t *testing.T) {
		var dst bytes.Buffer
		n, err := CopyWithContext(context.Background(), &dst, bytes.NewBufferString("hello"))
		require.NoError(t, err)
		require.Equal(t, int64(5), n)
		require.Equal(t, "hello", dst.String())
	})

	t.Run("canceled before copy", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		var dst bytes.Buffer
		n, err := CopyWithContext(ctx, &dst, bytes.NewBufferString("hello"))
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, int64(0), n)
		require.Empty(t, dst.String())
	})

	t.Run("canceled after read returns partial count", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		var dst bytes.Buffer

		n, err := CopyWithContext(ctx, &dst, &cancelAfterFirstRead{ctxCancel: cancel})
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, int64(5), n)
		require.Equal(t, "hello", dst.String())
	})

	t.Run("short write is returned", func(t *testing.T) {
		n, err := CopyWithContext(context.Background(), io.Discard, bytes.NewBuffer(nil))
		require.NoError(t, err)
		require.Equal(t, int64(0), n)

		shortWriter := writerFunc(func(p []byte) (int, error) { return 1, nil })
		n, err = CopyWithContext(context.Background(), shortWriter, bytes.NewBufferString("hello"))
		require.ErrorIs(t, err, io.ErrShortWrite)
		require.Equal(t, int64(1), n)
	})

	t.Run("reader error is returned", func(t *testing.T) {
		expected := errors.New("read failed")
		n, err := CopyWithContext(context.Background(), io.Discard, readerFunc(func([]byte) (int, error) {
			return 0, expected
		}))
		require.ErrorIs(t, err, expected)
		require.Equal(t, int64(0), n)
	})
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) {
	return f(p)
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
}
