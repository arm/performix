// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package ssh

import (
	"fmt"
	"io/fs"
	"os"
	"runtime"

	"github.com/spf13/afero"
	"golang.org/x/crypto/ssh"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type SSHKeyInfo struct {
	Path          string
	HasPassphrase bool
}

// GetPassphraselessKeyPaths returns the paths of the keys that do not have a passphrase
func GetPassphraselessKeyPaths(keys []SSHKeyInfo) []string {
	keyPaths := []string{}
	for _, hostKey := range keys {
		if !hostKey.HasPassphrase {
			keyPaths = append(keyPaths, hostKey.Path)
		}
	}
	return keyPaths
}

// ListHostPrivateKeys recursively lists all of the SSH private keys within the common search directories
func ListHostPrivateKeys(fileSys afero.Fs) []SSHKeyInfo {
	return ListPrivateKeys(fileSys, GetPrivateKeySearchDirs())
}

// ListPrivateKeys recursively lists all of the SSH private keys within the search directories
func ListPrivateKeys(fileSys afero.Fs, searchDirs []string) []SSHKeyInfo {
	foundKeys := []SSHKeyInfo{}
	for _, dir := range searchDirs {
		_ = afero.Walk(fileSys, dir, func(path string, f fs.FileInfo, err error) error {
			if err == nil {
				keyInfo, keyErr := ValidateSSHKey(fileSys, path)
				if keyErr == nil {
					foundKeys = append(foundKeys, keyInfo)
				}
			}
			return nil
		})
	}
	return foundKeys
}

// ValidateSSHKey returns an error if the file:
// - is not a parsable SSH private key,
// - doesn't have the correct permissions,
// - isn't owned by the current user (on non-Windows platforms).
// If the file is a valid key, it returns an SSHKeyInfo struct
func ValidateSSHKey(fs afero.Fs, path string) (SSHKeyInfo, error) {
	if path == "" {
		return SSHKeyInfo{}, message.New(message.CommonUnknownError).WithCause(fmt.Errorf("empty SSH key path provided"))
	}

	fileInfo, err := fs.Stat(path)

	// Check file exists and is a regular file
	if os.IsNotExist(err) {
		return SSHKeyInfo{}, message.New(message.EngineSshKeyFileNotFound).WithMetadata(map[string]string{"path": path}).WithCause(err)
	}

	// Validate the file as a private key
	keyData, err := afero.ReadFile(fs, path)
	if err != nil {
		return SSHKeyInfo{}, message.New(message.EngineSshKeyFileNotReadable).WithMetadata(map[string]string{"path": path}).WithCause(err)
	}

	hasPassphrase := false
	_, err = ssh.ParsePrivateKey(keyData)
	if err != nil {
		if _, ok := err.(*ssh.PassphraseMissingError); ok {
			hasPassphrase = true
		} else {
			return SSHKeyInfo{}, message.New(message.EngineSshKeyFileInvalid).WithMetadata(map[string]string{"path": path}).WithCause(err)
		}
	}

	// Check file has correct permissions and ownership
	// TODO - Support Windows
	if runtime.GOOS != "windows" {
		perm := fileInfo.Mode().Perm()
		if perm != perms.PrivateKeyPerm && perm != perms.PrivateKeyRoPerm {
			metadata := map[string]string{
				"path":        path,
				"permissions": fmt.Sprintf("%o", perm),
				"expected":    fmt.Sprintf("%s,%s", perms.PrivateKeyPermStr, perms.PrivateKeyRoPermStr),
			}
			return SSHKeyInfo{}, message.New(message.EngineSshWrongPermissions).WithMetadata(metadata)
		}
		if owner, user, err := util.IsFileOwnedByCurrUser(fileInfo); err != nil {
			metadata := map[string]string{"path": path, "owner": owner, "user": user}
			return SSHKeyInfo{}, message.New(message.EngineSshWrongOwnership).WithMetadata(metadata).WithCause(err)
		}
	}

	return SSHKeyInfo{
		Path:          path,
		HasPassphrase: hasPassphrase,
	}, nil
}
