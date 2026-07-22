// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/ssh"
)

func TestSSHKeyGeneration(t *testing.T) {
	t.Run("Check generated key is valid", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create temp file for key
		publicKeyPath := filepath.Join(tempDir, "example_ssh_key")
		privateKeyPath := filepath.Join(tempDir, "example_ssh_key.pub")
		err := MakeSSHKeyPair(publicKeyPath, privateKeyPath, "")

		privateKeyData, _ := os.ReadFile(privateKeyPath)
		publicKeyData, _ := os.ReadFile(publicKeyPath)
		assert.NoError(t, err)

		// Check generated keys are valid keys
		_, err = ssh.ParsePrivateKey(privateKeyData)
		assert.NoError(t, err)
		_, _, _, _, err = ssh.ParseAuthorizedKey(publicKeyData)
		assert.NoError(t, err)
	})
}
