// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package pidfiles

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ARM-software/golang-utils/utils/filesystem"
	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
)

// Returns path to PID file.
func ConstructPidFilePath(host string, port int) (string, error) {
	stateDir, err := userdirs.StateDir()
	if err != nil {
		return "", err
	}

	host = strings.ReplaceAll(host, ".", "-")
	host = strings.ReplaceAll(host, ":", "-")
	fileName := fmt.Sprintf("%v_%v_%v.pid", terminology.GetDaemonDirName(), host, port)
	// Where to store the PID of daemon process
	return filepath.Abs(filepath.Join(stateDir, fileName))
}

// Read the PID from a pidfile
func GetPid(pidfile string) (int, error) {
	data, err := filesystem.ReadFile(pidfile)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}

// Write the PID of the daemon process into a file.
// param PID :: The PID to write into file
func SavePid(pid int, host string, port int) error {
	pidFilePath, err := ConstructPidFilePath(host, port)
	if err != nil {
		return err
	}

	if filesystem.Exists(pidFilePath) {
		log.WithFields(log.Fields{
			"PID file": pidFilePath,
		}).Info("Duplicate PID file found. Overwriting.")
	}

	return writeFile(pidFilePath, strconv.Itoa(pid))
}

func DeletePid(host string, port int) {
	pidFilePath, err := ConstructPidFilePath(host, port)
	if err == nil {
		err = filesystem.Rm(pidFilePath)
	}
	if err != nil {
		log.WithFields(log.Fields{
			"PID file": pidFilePath,
			"err":      err,
		}).Error("Could not remove PID file")
	}
}

func writeFile(path string, content string) error {
	err := filesystem.MkDir(filepath.Dir(path))
	if err != nil {
		return err
	}

	file, err := filesystem.CreateFile(path)
	if err != nil {
		return err
	}

	defer file.Close()

	if _, err := file.WriteString(content); err != nil {
		return err
	}

	return file.Sync() // flush to disk
}
