// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"golang.org/x/term"
)

// PromptLine reads a line from stdin while echoing input if stdin is a TTY.
func PromptLine(prompt string) (string, error) {
	stdin := os.Stdin
	stdinFD := int(stdin.Fd())

	// If stdin is not a terminal, read directly from stdin (supports piping).
	if !term.IsTerminal(stdinFD) {
		buf, err := ReadUntilNewline(stdin)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(string(buf), "\r"), nil
	}

	// stdin is a terminal; prefer /dev/tty on Unix for a clean prompt.
	reader := io.Reader(stdin)
	if runtime.GOOS != "windows" {
		if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
			reader = tty
			defer tty.Close()
		}
	}

	// Always write the prompt to stderr
	fmt.Fprint(os.Stderr, prompt)

	bufReader := bufio.NewReader(reader)
	line, err := bufReader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// ReadUntilNewline reads from reader until a newline or EOF.
func ReadUntilNewline(reader io.Reader) ([]byte, error) {
	var buf []byte
	for {
		var b [1]byte
		n, err := reader.Read(b[:])
		if n > 0 {
			if b[0] == '\n' {
				break
			}
			buf = append(buf, b[:n]...)
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
	}
	return buf, nil
}
