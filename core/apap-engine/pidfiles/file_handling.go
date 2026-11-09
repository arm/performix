// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package pidfiles

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
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
	data, err := os.ReadFile(pidfile)
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

	if _, err := os.Stat(pidFilePath); err == nil {
		log.WithFields(log.Fields{
			"PID file": pidFilePath,
		}).Info("Duplicate PID file found. Overwriting.")
	}

	return writeFile(pidFilePath, strconv.Itoa(pid))
}

func DeletePid(host string, port int) {
	pidFilePath, err := ConstructPidFilePath(host, port)
	if err == nil {
		err = os.Remove(pidFilePath)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.WithFields(log.Fields{
			"PID file": pidFilePath,
			"err":      err,
		}).Error("Could not remove PID file")
	}
}

func writeFile(path string, content string) error {
	err := os.MkdirAll(filepath.Dir(path), perms.LocalDirPerm)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perms.LocalFilePerm)
	if err != nil {
		return err
	}

	defer file.Close()

	if _, err := file.WriteString(content); err != nil {
		return err
	}

	return file.Sync() // flush to disk
}
