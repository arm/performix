// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ARM-software/golang-utils/utils/filesystem"
	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
)

var logLevels = map[string]log.Level{
	"trace": log.TraceLevel,
	"debug": log.DebugLevel,
	"info":  log.InfoLevel,
	"warn":  log.WarnLevel,
	"error": log.ErrorLevel,
	"fatal": log.FatalLevel,
	"panic": log.PanicLevel,
}

func createFile(path string) error {
	err := filesystem.MkDir(filepath.Dir(path))
	if err != nil {
		return err
	}

	if !filesystem.Exists(path) {
		f, err := filesystem.CreateFile(path)
		if err != nil {
			return err
		}
		f.Close()
	}

	return nil
}

func GetNewLogFilePath() (string, error) {
	var logFileName string
	var err error

	logFileDirectory, err := userdirs.StateDir()
	if err == nil {
		logFileName = filepath.Join(
			logFileDirectory,
			time.Now().Format("2006-Jan-02_15-04-05.log"),
		)
	}

	return logFileName, err
}

func CreateNewLogFile() (string, error) {
	logFilePath, err := GetNewLogFilePath()
	if err != nil {
		return "", err
	}

	err = createFile(logFilePath)
	if err != nil {
		return "", err
	}

	return logFilePath, nil
}

func SetLogOutputFile(fileName string) (*os.File, error) {
	if err := createFile(fileName); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(fileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, perms.LocalFilePerm)
	if err == nil {
		log.SetOutput(file)
	}
	return file, err
}

func SetLogLevel(level string) error {
	logLevel, valid := logLevels[level]
	if !valid {
		return fmt.Errorf("unrecognized log level: %s", level)
	}
	log.SetLevel(logLevel)
	return nil
}

func ReadLogFile(file string) (string, error) {
	content, err := filesystem.ReadFile(file)
	if err != nil {
		return "", err
	}
	return string(content), nil
}
