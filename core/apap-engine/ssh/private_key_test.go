// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package ssh

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const privateKey = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACDd6++9+BGQof1flAvmZp5Q5FGTCEuf7iQCBv1vc7FFhAAAAJjiLZu94i2b
vQAAAAtzc2gtZWQyNTUxOQAAACDd6++9+BGQof1flAvmZp5Q5FGTCEuf7iQCBv1vc7FFhA
AAAEBAmohJc7qcs8fyFTI8/ez3iHkdSM7SKwv3RgH+JVcIyt3r7734EZCh/V+UC+ZmnlDk
UZMIS5/uJAIG/W9zsUWEAAAAEG1hcmpvaDAzQGUxMzQ2NDMBAgMEBQ==
-----END OPENSSH PRIVATE KEY-----`

const passphraseProtectedPrivateKey = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAACmFlczI1Ni1jdHIAAAAGYmNyeXB0AAAAGAAAABDq1Yv8ZX
IqLmdXsD+RyxX6AAAAGAAAAAEAAAAzAAAAC3NzaC1lZDI1NTE5AAAAIMdxTeo0p+t/feLs
80klzFeK8RR5+lP+evglssXzwMtZAAAAoE5OPK+gJDjTZf46KpUeu7tUq9EHjrkZqwGWf0
g5meQMGo9mwBkHGP0IED5P7XZqepIE3E3Pd8mkOTfEG4isGdC1YG9PBSbxFJ3BBGymz9yK
F8ozorOYrgkK8nVcE8yZ7Fs/3t3tvwNV2RgSngp0bbxOGzdt9TAXGhMMiRm5nChAgZNnXf
F3bYTnse8mfxbEWf/RnkD7uFdlpb9H+POFJrw=
-----END OPENSSH PRIVATE KEY-----`

func TestListCommand(t *testing.T) {
	t.Run("invalid keys are ignored", func(t *testing.T) {
		fs := afero.NewOsFs()
		tempDir := t.TempDir()
		err := afero.WriteFile(fs, filepath.Join(tempDir, "/id_rsa"), []byte("dummy key content"), perms.PrivateKeyPerm)
		assert.NoError(t, err)

		privateKeys := ListPrivateKeys(fs, []string{tempDir})

		assert.Equal(t, privateKeys, []SSHKeyInfo{})
	})

	t.Run("valid keys are found in base and nested directories", func(t *testing.T) {
		fs := afero.NewOsFs()
		tempDir := t.TempDir()

		pt := filepath.Join(tempDir, "/.ssh/nested")
		err := fs.MkdirAll(pt, perms.SshDirPerm)
		assert.NoError(t, err)
		err = os.WriteFile(filepath.Join(tempDir, "/.ssh/id_rsa"), []byte(privateKey), perms.PrivateKeyPerm)
		assert.NoError(t, err)
		err = os.WriteFile(filepath.Join(tempDir, "/.ssh/nested/neo"), []byte(privateKey), perms.PrivateKeyPerm)
		assert.NoError(t, err)

		keys := ListPrivateKeys(fs, []string{tempDir})

		assert.Equal(t, 2, len(keys))
		assert.NotEqual(t, util.Find(keys, func(i int) bool { return keys[i].Path == filepath.Join(tempDir, "/.ssh/id_rsa") }), -1)
		assert.NotEqual(t, util.Find(keys, func(i int) bool { return keys[i].Path == filepath.Join(tempDir, "/.ssh/nested/neo") }), -1)
	})

	t.Run("passphrase status is preserved", func(t *testing.T) {
		fs := afero.NewOsFs()
		tempDir := t.TempDir()

		err := fs.MkdirAll(filepath.Join(tempDir, "/.ssh"), perms.SshDirPerm)
		assert.NoError(t, err)
		err = os.WriteFile(filepath.Join(tempDir, "/.ssh/id_rsa"), []byte(privateKey), perms.PrivateKeyPerm)
		assert.NoError(t, err)
		err = os.WriteFile(filepath.Join(tempDir, "/.ssh/id_ed25519"), []byte(passphraseProtectedPrivateKey), perms.PrivateKeyPerm)
		assert.NoError(t, err)

		keys := ListPrivateKeys(fs, []string{tempDir})

		assert.Equal(t, 2, len(keys))
		idxPlain := util.Find(keys, func(i int) bool { return keys[i].Path == filepath.Join(tempDir, "/.ssh/id_rsa") })
		idxPassphrase := util.Find(keys, func(i int) bool { return keys[i].Path == filepath.Join(tempDir, "/.ssh/id_ed25519") })
		assert.NotEqual(t, -1, idxPlain)
		assert.NotEqual(t, -1, idxPassphrase)
		assert.False(t, keys[idxPlain].HasPassphrase)
		assert.True(t, keys[idxPassphrase].HasPassphrase)
	})

	t.Run("no keys are found when search directory is invalid", func(t *testing.T) {
		fs := afero.NewOsFs()
		keys := ListPrivateKeys(fs, []string{"/__randomDir__X321PUO"})
		assert.Equal(t, 0, len(keys))
	})
}

func TestGetPassphraselessKeyPaths(t *testing.T) {
	keys := []SSHKeyInfo{
		{Path: "ssh/keyA", HasPassphrase: true},
		{Path: "etc/keyB", HasPassphrase: false},
		{Path: "etc/keyC", HasPassphrase: false},
	}

	paths := GetPassphraselessKeyPaths(keys)

	assert.Equal(t, []string{"etc/keyB", "etc/keyC"}, paths)
}

func TestValidateSSHKeyPermissions(t *testing.T) {
	fs := afero.NewOsFs()
	tempDir := t.TempDir()
	keyPath := filepath.Join(tempDir, "id_rsa")
	err := afero.WriteFile(fs, keyPath, []byte(privateKey), perms.PrivateKeyPerm)
	require.NoError(t, err)

	t.Run("accepts 0600", func(t *testing.T) {
		_, err := ValidateSSHKey(fs, keyPath)
		assert.NoError(t, err)
	})

	t.Run("accepts 0400", func(t *testing.T) {
		require.NoError(t, fs.Chmod(keyPath, perms.PrivateKeyRoPerm))
		_, err := ValidateSSHKey(fs, keyPath)
		assert.NoError(t, err)
	})

	t.Run("rejects too-permissive", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("permission checks are skipped on Windows")
		}
		require.NoError(t, fs.Chmod(keyPath, 0o644))
		_, err := ValidateSSHKey(fs, keyPath)
		var msgErr message.Message
		require.ErrorAs(t, err, &msgErr)
		assert.Equal(t, message.EngineSshWrongPermissions, msgErr.Code())
		assert.Equal(t, perms.PrivateKeyPermStr+","+perms.PrivateKeyRoPermStr, msgErr.Metadata()["expected"])
	})
}

func TestValidateSSHKeyPassphrase(t *testing.T) {
	fs := afero.NewOsFs()
	tempDir := t.TempDir()
	keyPath := filepath.Join(tempDir, "id_ed25519")
	err := afero.WriteFile(fs, keyPath, []byte(passphraseProtectedPrivateKey), perms.PrivateKeyPerm)
	require.NoError(t, err)

	keyInfo, err := ValidateSSHKey(fs, keyPath)

	assert.NoError(t, err)
	assert.Equal(t, keyPath, keyInfo.Path)
	assert.True(t, keyInfo.HasPassphrase)
}
