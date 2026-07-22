// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package logging

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
)

func TestGetNewLogFilePath(t *testing.T) {
	t.Run("log file path uses user's state directory", func(t *testing.T) {
		want, _ := userdirs.StateDir()
		got, _ := GetNewLogFilePath()

		assert.Equal(t, want, filepath.Dir(got))
	})

	t.Run("log file path name is formatted with a timestamp", func(t *testing.T) {
		logFilePath, _ := GetNewLogFilePath()

		_, err := time.Parse("2006-Jan-02_15-04-05.log", filepath.Base(logFilePath))
		assert.NoError(t, err, "File name not formatted correctly")
	})
}

func TestSetLogOutputFile(t *testing.T) {
	t.Run("creates directory if it does not exist", func(t *testing.T) {
		// Set up a path in a directory that does not exist
		tempDir := filepath.Join(t.TempDir(), "nonexistent-dir")
		logFile := filepath.Join(tempDir, "log.txt")

		// Try to set the log output file
		file, err := SetLogOutputFile(logFile)

		assert.NotNil(t, file)
		assert.NoError(t, err)

		if file != nil {
			defer file.Close()
		}

		// It should create the directory
		_, err = os.Stat(tempDir)
		assert.NoError(t, err)
	})
}

func TestCreateFile(t *testing.T) {
	t.Run("Creates a new file in given location", func(t *testing.T) {
		tempDir := filepath.Join(os.TempDir(), "foo")
		defer os.RemoveAll(tempDir)
		filePath := filepath.Join(tempDir, "bar.txt")

		err := createFile(filePath)

		assert.NoError(t, err)
		_, err = os.Stat(filePath)
		assert.NoError(t, err)
	})

	t.Run("Does nothing if file already exists", func(t *testing.T) {
		tempDir := filepath.Join(os.TempDir(), "foo")
		defer os.RemoveAll(tempDir)
		filePath := filepath.Join(tempDir, "bar.txt")

		err := createFile(filePath)
		assert.NoError(t, err)

		err = createFile(filePath)
		assert.NoError(t, err)
	})
}

func TestSetLogLevel(t *testing.T) {
	testTable := []struct {
		level         string
		expectError   bool
		expectedLevel log.Level
	}{
		{"trace", false, log.TraceLevel},
		{"debug", false, log.DebugLevel},
		{"info", false, log.InfoLevel},
		{"warn", false, log.WarnLevel},
		{"error", false, log.ErrorLevel},
		{"fatal", false, log.FatalLevel},
		{"panic", false, log.PanicLevel},
		{"erroneous", true, log.PanicLevel},
		{"Trace", true, log.PanicLevel},
	}

	for _, test := range testTable {
		err := SetLogLevel(test.level)
		if test.expectError {
			assert.NotNil(t, err)
		} else {
			assert.True(t, log.IsLevelEnabled(test.expectedLevel))
		}
	}
}

func TestSetAndReadLogFile(t *testing.T) {
	testFile := filepath.Join(os.TempDir(), "log.test")
	file, err := SetLogOutputFile(testFile)
	assert.NoError(t, err)
	log.SetLevel(log.InfoLevel)
	log.Print("TestingTestingTesting")
	actual, err := ReadLogFile(testFile)
	assert.NoError(t, err)
	assert.Regexp(t, ".* level=info msg=TestingTestingTesting", actual)
	file.Close()
	err = os.Remove(testFile)
	assert.NoError(t, err)
}
