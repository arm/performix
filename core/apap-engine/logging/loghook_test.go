// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestDeferredFileOpenLogHook(t *testing.T) {
	t.Run("Messages written before swapping to file are written to file", func(t *testing.T) {
		logger := log.New()
		hook := NewDeferredFileOpenLogHook(&log.TextFormatter{})
		defer hook.Close()
		logger.AddHook(hook)

		logger.Error("foobar")

		dir := t.TempDir()
		fileName := filepath.Join(dir, "test.log")
		err := hook.SwapToFile(fileName)
		if err != nil {
			assert.NoError(t, err)
			return
		}

		assert.FileExists(t, fileName)
		data, err := os.ReadFile(fileName)
		if err != nil {
			assert.NoError(t, err)
			return
		}

		logStr := string(data)
		lines := strings.Split(logStr, "\n")
		assert.Contains(t, logStr, "foobar")
		assert.Equal(t, 2, len(lines))
	})

	t.Run("Swapping to file is idempotent for the same filename", func(t *testing.T) {
		logger := log.New()
		hook := NewDeferredFileOpenLogHook(&log.TextFormatter{})
		defer hook.Close()
		logger.AddHook(hook)

		logger.Error("foobar")

		dir := t.TempDir()
		fileName := filepath.Join(dir, "test.log")
		err := hook.SwapToFile(fileName)
		if err != nil {
			assert.NoError(t, err)
			return
		}

		err = hook.SwapToFile(fileName)
		if err != nil {
			assert.NoError(t, err)
			return
		}
	})

	t.Run("Swapping to file happens once only, even if supplied with a different filename", func(t *testing.T) {
		logger := log.New()
		hook := NewDeferredFileOpenLogHook(&log.TextFormatter{})
		defer hook.Close()
		logger.AddHook(hook)

		logger.Error("foobar")

		dir := t.TempDir()
		fileName := filepath.Join(dir, "test.log")
		err := hook.SwapToFile(fileName)
		if err != nil {
			assert.NoError(t, err)
			return
		}

		fileName2 := filepath.Join(dir, "test2.log")
		err = hook.SwapToFile(fileName2)
		if err != nil {
			assert.ErrorContains(t, err, "log already opened with a different filename")
			return
		}

		assert.NoFileExists(t, fileName2)
	})

	t.Run("Messages written after swapping to file are written to file", func(t *testing.T) {
		logger := log.New()
		hook := NewDeferredFileOpenLogHook(&log.TextFormatter{})
		defer hook.Close()
		logger.AddHook(hook)

		logger.Error("foobar")

		dir := t.TempDir()
		fileName := filepath.Join(dir, "test.log")
		err := hook.SwapToFile(fileName)
		if err != nil {
			assert.NoError(t, err)
			return
		}

		logger.Error("qwerty")

		assert.FileExists(t, fileName)
		data, err := os.ReadFile(fileName)
		if err != nil {
			assert.NoError(t, err)
			return
		}

		logStr := string(data)
		lines := strings.Split(logStr, "\n")
		assert.Contains(t, logStr, "foobar")
		assert.Contains(t, logStr, "qwerty")
		assert.Equal(t, 3, len(lines))
	})
}
