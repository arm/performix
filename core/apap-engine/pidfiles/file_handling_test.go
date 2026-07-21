// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package pidfiles

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
)

func TestConstructPidFilePath(t *testing.T) {
	stateDir, _ := userdirs.StateDir()
	testTable := []struct {
		host  string
		port  int
		fPath string
	}{
		{"hello", 2, "hello_2.pid"},
		{"file-name", 34, "file-name_34.pid"},
		{"file-name", -1, "file-name_-1.pid"},
		{"127.0.0.1", 333, "127-0-0-1_333.pid"},
		{"2001:0db8:85a3:0000:0000:8a2e:0370:7334", 22, "2001-0db8-85a3-0000-0000-8a2e-0370-7334_22.pid"},
		{"2001:db8:0:0:1::1", 9020, "2001-db8-0-0-1--1_9020.pid"},
	}

	for _, test := range testTable {
		result, _ := ConstructPidFilePath(test.host, test.port)
		expectedResult := filepath.Join(stateDir, fmt.Sprintf("%v_%v", terminology.GetDaemonDirName(), test.fPath))
		assert.Equal(t, expectedResult, result)
	}
}

func TestSaveAndGetPid(t *testing.T) {
	t.Run("Save pid to a file and get the pid", func(t *testing.T) {
		host := "78"
		port := 90
		expectedPidFilePath, _ := ConstructPidFilePath(host, port)
		pid := 123456

		err := SavePid(pid, host, port)
		assert.NoError(t, err)

		pidInFile, err := GetPid(expectedPidFilePath)
		assert.Equal(t, pid, pidInFile)
		assert.NoError(t, err)

		DeletePid(host, port)
	})

	t.Run("Overwrite previous pid file", func(t *testing.T) {
		host := "78"
		port := 90
		expectedPidFilePath, _ := ConstructPidFilePath(host, port)
		pid := 123456

		err := SavePid(2456, host, port)
		assert.NoError(t, err)
		err = SavePid(pid, host, port)
		assert.NoError(t, err)

		pidInFile, err := GetPid(expectedPidFilePath)
		assert.Equal(t, pid, pidInFile)
		assert.NoError(t, err)

		DeletePid(host, port)
	})

	t.Run("Reading a nonexistent pid file", func(t *testing.T) {
		_, err := GetPid("bogus_file.pid")
		assert.Error(t, err)
	})
}

func TestDeletePid(t *testing.T) {
	host := "some-host"
	port := 1

	pidFilePath, err := ConstructPidFilePath(host, port)
	assert.NoError(t, err)

	file, err := os.Create(pidFilePath)
	assert.NoError(t, err)
	file.Close()

	DeletePid(host, port)

	assert.NoFileExists(t, pidFilePath)
}
