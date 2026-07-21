// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

func MakeSSHKeyPair(pubKeyPath, privateKeyPath string, passphrase string) error {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate rsa key: %w", err)
	}

	// Create new file for private key
	privateKeyFile, err := os.Create(privateKeyPath)
	if err != nil {
		return fmt.Errorf("failed to create private key: %w", err)
	}
	defer privateKeyFile.Close()

	// Set file permissions
	if err := os.Chmod(privateKeyPath, perms.PrivateKeyPerm); err != nil {
		return fmt.Errorf("failed to set permissions on private key: %w", err)
	}

	// Generate PEM data for private key
	privateKeyPEM := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}

	// Encrypt the private key if a passphrase was specified
	if len(passphrase) > 0 {
		//nolint:staticcheck // SA1019 - this is only temporary code, will be removed when we support password protected keys
		privateKeyPEM, err = x509.EncryptPEMBlock(rand.Reader, privateKeyPEM.Type, privateKeyPEM.Bytes, []byte(passphrase), x509.PEMCipherAES256)
		if err != nil {
			return fmt.Errorf("failed to encrypt private key: %w", err)
		}
	}

	// Write private key PEM data to file
	if err := pem.Encode(privateKeyFile, privateKeyPEM); err != nil {
		return fmt.Errorf("failed to encode private key: %w", err)
	}

	// Create the corresponding public key
	pub, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return fmt.Errorf("failed to create public key: %w", err)
	}

	return os.WriteFile(pubKeyPath, ssh.MarshalAuthorizedKey(pub), perms.PublicKeyPerm)
}
