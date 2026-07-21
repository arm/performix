// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptLineReadsFromStdinWhenNotTTY(t *testing.T) {
	origStdin := os.Stdin
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		os.Stdin = origStdin
		_ = r.Close()
		_ = w.Close()
	})

	_, err = w.Write([]byte("hello\r\n"))
	require.NoError(t, err)
	_ = w.Close()

	os.Stdin = r

	line, err := PromptLine("Prompt: ")
	require.NoError(t, err)
	assert.Equal(t, "hello", line)
}

func TestReadUntilNewlineReturnsEmptyOnEOF(t *testing.T) {
	buf, err := ReadUntilNewline(errReader{err: io.EOF})
	require.NoError(t, err)
	assert.Equal(t, []byte(nil), buf)
}

func TestReadUntilNewlineReturnsErrorOnReadFailure(t *testing.T) {
	readErr := io.ErrClosedPipe
	buf, err := ReadUntilNewline(errReader{err: readErr})
	require.ErrorIs(t, err, readErr)
	assert.Nil(t, buf)
}

type errReader struct {
	err error
}

func (e errReader) Read(_ []byte) (int, error) {
	return 0, e.err
}
