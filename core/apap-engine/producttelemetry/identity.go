// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package producttelemetry

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

const userIDFile = "telemetry-user-id"

// GetOrCreateUserID returns the persistent per-user identity, including for CLI-only usage.
func GetOrCreateUserID(directory string) (string, error) {
	path := filepath.Join(directory, userIDFile)
	id, err := readUserID(path)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(directory, perms.LocalDirPerm); err != nil {
		return "", err
	}
	// Publish a complete file without replacing another daemon's identity.
	file, err := os.CreateTemp(directory, ".telemetry-user-id-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	id = uuid.NewString()
	_, writeErr := file.WriteString(id + "\n")
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return "", err
	}
	if err := os.Link(file.Name(), path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return readUserID(path)
		}
		return "", err
	}
	return id, nil
}

func readUserID(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(data))
	if err := uuid.Validate(id); err != nil {
		return "", err
	}
	return id, nil
}
