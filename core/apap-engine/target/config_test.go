// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const testTargetFilename = "test_targets.json"

type mockLocalhostVerifier struct {
	mock.Mock
}

type badTarget struct{}

func (b *badTarget) DisplayHost() string                       { return "bad" }
func (b *badTarget) GetUserDataDirectoryName() (string, error) { return "bad", nil }
func (b *badTarget) String() string                            { return "bad" }
func (b *badTarget) Validate(name string) error                { return nil }

type MockTargetManager struct {
	mock.Mock
}

func (m *MockTargetManager) ReadTargetConfig() (TargetConfig, error) {
	mockArgs := m.Called()
	return mockArgs.Get(0).(TargetConfig), mockArgs.Error(1)
}

func (mlhv *mockLocalhostVerifier) IsLocalhostSupported() bool {
	args := mlhv.Called()
	return args.Bool(0)
}

func checkFileContains(filepath string, substring string) error {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return err
	}

	content := string(data)

	if !strings.Contains(content, substring) {
		return errors.New("file does not contain substring: " + substring)
	}

	return nil
}

func makeSimpleSSHTarget(host string, port int32, username string, privateKeyFilename string) Target {
	return &SSHTarget{Jumps: []SSHHostConfig{
		{Host: host, Port: port, Username: username, PrivateKeyFilename: privateKeyFilename},
	}}
}

func makeExampleJumpSSHTarget(privateKeyFilename string) Target {
	return &SSHTarget{Jumps: []SSHHostConfig{
		{Host: "1.1.1.1", Port: 1, Username: "Hop1", PrivateKeyFilename: privateKeyFilename},
		{Host: "2.2.2.2", Port: 2, Username: "Hop2", PrivateKeyFilename: privateKeyFilename},
		{Host: "3.3.3.3", Port: 3, Username: "Hop3", PrivateKeyFilename: privateKeyFilename},
		{Host: "4.4.4.4", Port: 4, Username: "Hop4", PrivateKeyFilename: privateKeyFilename},
		{Host: "5.5.5.5", Port: 5, Username: "finalHop", PrivateKeyFilename: privateKeyFilename},
	}}
}

func SetupTargetDirectory(t *testing.T, mlhv *mockLocalhostVerifier) (TargetManagerService, string, string, string) {
	tempDir := t.TempDir()
	testTargetFilepath := filepath.Join(tempDir, testTargetFilename)
	targetService := NewTargetManager(testTargetFilepath, mlhv)

	// Create temp SSH key pair without passphrase
	privateKeyFile := filepath.Join(tempDir, "example_ssh_key")
	publicKeyFile := filepath.Join(tempDir, "example_ssh_key.pub")
	err := util.MakeSSHKeyPair(publicKeyFile, privateKeyFile, "")
	if err != nil {
		t.Fatal("failure creating ssh key pair")
	}

	// Create temp SSH key pair with passphrase
	privateKeyFilePw := filepath.Join(tempDir, "example_ssh_key_pw")
	publicKeyFilePw := filepath.Join(tempDir, "example_ssh_key_pw.pub")
	err = util.MakeSSHKeyPair(publicKeyFilePw, privateKeyFilePw, "super_secure_passphrase")
	if err != nil {
		t.Fatal("failure creating ssh key pair with passphrase")
	}

	return targetService, testTargetFilepath, privateKeyFile, privateKeyFilePw
}

func TestAddTarget(t *testing.T) {
	mlhv := mockLocalhostVerifier{}
	mlhv.On("IsLocalhostSupported").Return(false)
	targetService, testTargetFilepath, sshKeyFile, sshKeyFileWithPw := SetupTargetDirectory(t, &mlhv)

	t.Run("Add target succeeds with valid arguments", func(t *testing.T) {
		err := targetService.AddTarget("valid_ip", makeSimpleSSHTarget("111.111.111.111", 22, "tester", sshKeyFile))
		assert.NoError(t, err)
		assert.NoError(t, checkFileContains(testTargetFilepath, "111.111.111.111"))

		err = targetService.AddTarget("valid_host", makeSimpleSSHTarget("super-cool-host.com", 22, "tester", sshKeyFile))
		assert.NoError(t, err)
		assert.NoError(t, checkFileContains(testTargetFilepath, "super-cool-host.com"))

		err = targetService.AddTarget("a_host", makeSimpleSSHTarget("a", 22, "tester", sshKeyFile))
		assert.NoError(t, err)
		assert.NoError(t, checkFileContains(testTargetFilepath, "a"))
	})

	t.Run("Add target succeeds for a jump host target with valid arguments", func(t *testing.T) {
		myJumpTarget := makeExampleJumpSSHTarget(sshKeyFile)

		err := targetService.AddTarget("my_jump_target", myJumpTarget)
		assert.NoError(t, err)
		assert.NoError(t, checkFileContains(testTargetFilepath, "my_jump_target"))
	})

	t.Run("Add target succeeds for Android target with valid arguments", func(t *testing.T) {
		deviceIP := "android-target.invalid:5555"
		err := targetService.AddTarget("my_android_target", &AndroidTarget{SerialNumber: "device-123", DeviceIPAddress: &deviceIP})
		assert.NoError(t, err)
		assert.NoError(t, checkFileContains(testTargetFilepath, `"type": "android"`))
		assert.NoError(t, checkFileContains(testTargetFilepath, `"serial_number": "device-123"`))
	})

	t.Run("Add target fails with invalid name localhost", func(t *testing.T) {
		err := targetService.AddTarget(LocalhostName, makeSimpleSSHTarget("111.111.111.111", 22, "tester", sshKeyFile))
		assert.ErrorContains(t, err, message.EngineTargetConfigNameReserved)

	})

	t.Run("Add target fails with duplicate names", func(t *testing.T) {
		err := targetService.AddTarget("duplicate_host", makeSimpleSSHTarget("111.111.111.111", 22, "tester", sshKeyFile))
		assert.NoError(t, err)
		assert.NoError(t, checkFileContains(testTargetFilepath, "111.111.111.111"))
		err = targetService.AddTarget("duplicate_host", makeSimpleSSHTarget("super-cool-host.com", 22, "tester", sshKeyFile))
		assert.ErrorContains(t, err, message.EngineTargetConfigAlreadyExists)
	})

	t.Run("Add target fails with invalid host", func(t *testing.T) {
		err := targetService.AddTarget("invalid_host1", makeSimpleSSHTarget("not a valid host name", 22, "tester", "."))
		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EngineTargetConfigInvalidHostFormat, msgErr.Code())
		assert.Equal(t, "not a valid host name", msgErr.Metadata()["hostAddress"])
		assert.Equal(t, "tester@not a valid host name", msgErr.Metadata()["jumpNode"])

		err = targetService.AddTarget("invalid_host2", makeSimpleSSHTarget("invalid{()';$\\}", 22, "tester", "."))
		ok = errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EngineTargetConfigInvalidHostFormat, msgErr.Code())
		assert.Equal(t, "invalid{()';$\\}", msgErr.Metadata()["hostAddress"])
		assert.Equal(t, "tester@invalid{()';$\\}", msgErr.Metadata()["jumpNode"])

		err = targetService.AddTarget("empty_host", makeSimpleSSHTarget("", 22, "tester", "."))
		ok = errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EngineTargetConfigInvalidHostFormat, msgErr.Code())
		assert.Equal(t, "", msgErr.Metadata()["hostAddress"])
		assert.Equal(t, "tester@<unknown>", msgErr.Metadata()["jumpNode"])
	})

	t.Run("Add Android target fails with invalid serial number", func(t *testing.T) {
		err := targetService.AddTarget("invalid_android", &AndroidTarget{SerialNumber: "not a serial"})
		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EngineTargetConfigInvalidHostFormat, msgErr.Code())
		assert.Equal(t, "not a serial", msgErr.Metadata()["hostAddress"])
		assert.Equal(t, "Android target", msgErr.Metadata()["jumpNode"])
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("Add Android target fails with invalid device IP address", func(t *testing.T) {
		deviceIP := "not a device address"
		err := targetService.AddTarget("invalid_android", &AndroidTarget{SerialNumber: "device-123", DeviceIPAddress: &deviceIP})
		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EngineTargetConfigInvalidHostFormat, msgErr.Code())
		assert.Equal(t, "device-123@not a device address", msgErr.Metadata()["hostAddress"])
		assert.Equal(t, "Android target", msgErr.Metadata()["jumpNode"])
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("Add target fails with invalid port", func(t *testing.T) {
		err := targetService.AddTarget("foo", makeSimpleSSHTarget("111.111.111.111", -42, "tester", "."))
		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EngineCommonInvalidPortFormat, msgErr.Code())
		assert.Equal(t, "-42", msgErr.Metadata()["portNum"])
		assert.Equal(t, "tester@111.111.111.111:-42", msgErr.Metadata()["jumpNode"])
	})

	t.Run("Add target fails with invalid SSH key path", func(t *testing.T) {
		err := targetService.AddTarget("bar", makeSimpleSSHTarget("111.111.111.111", 22, "tester", "definitely_not/a_real/file/tester.key"))

		assert.ErrorContains(t, err, message.EngineSshKeyFileNotFound)
		// Ensure key information is not printed to console
		assert.False(t, strings.Contains(err.Error(), "definitely_not/a_real/file/tester.json"))
	})

	t.Run("Add target fails with invalid SSH key permissions", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			_ = os.Chmod(sshKeyFile, 0777)
			err := targetService.AddTarget("bar", makeSimpleSSHTarget("111.111.111.111", 22, "tester", sshKeyFile))
			assert.ErrorContains(t, err, message.EngineSshWrongPermissions)
		}
	})

	t.Run("Add target fails with invalid SSH key file", func(t *testing.T) {
		_ = os.Chmod(sshKeyFile, perms.PrivateKeyPerm)
		_ = os.WriteFile(sshKeyFile, []byte("invalid ssh key"), perms.PrivateKeyPerm)
		err := targetService.AddTarget("bar", makeSimpleSSHTarget("111.111.111.111", 22, "tester", sshKeyFile))
		assert.ErrorContains(t, err, message.EngineSshKeyFileInvalid)
	})

	t.Run("Add target succeeds with passphrase protected SSH key", func(t *testing.T) {
		_ = os.Chmod(sshKeyFileWithPw, perms.PrivateKeyPerm)
		err := targetService.AddTarget("passphrase_key_target", makeSimpleSSHTarget("111.111.111.111", 22, "tester", sshKeyFileWithPw))
		assert.NoError(t, err)
		assert.NoError(t, checkFileContains(testTargetFilepath, "passphrase_key_target"))
	})
}

func TestWriteTargetConfigConversionErrorIncludesPathMetadata(t *testing.T) {
	mlhv := mockLocalhostVerifier{}
	mlhv.On("IsLocalhostSupported").Return(false)
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "targets.json")
	manager := NewTargetManager(configPath, &mlhv)

	cfg := &TargetConfig{
		Targets: map[string]Target{
			"bad": &badTarget{},
		},
	}

	err := manager.writeTargetConfig(cfg)
	require.Error(t, err)

	var msg message.Message
	require.True(t, errors.As(err, &msg))
	require.Equal(t, message.EngineTargetConfigWriteFailure, msg.Code())
	meta := msg.Metadata()
	require.Equal(t, configPath, meta["path"])
}

func TestWriteTargetConfigWriteFailureIncludesPathMetadata(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping permission-based test on Windows")
	}

	mlhv := mockLocalhostVerifier{}
	mlhv.On("IsLocalhostSupported").Return(false)

	tempDir := t.TempDir()
	roDir := filepath.Join(tempDir, "ro")
	require.NoError(t, os.MkdirAll(roDir, 0o555))
	t.Cleanup(func() {
		_ = os.Chmod(roDir, 0o755)
	})

	configPath := filepath.Join(roDir, "targets.json")
	manager := NewTargetManager(configPath, &mlhv)

	cfg := &TargetConfig{
		Targets: map[string]Target{
			"localhost": &LocalTarget{},
		},
	}

	err := manager.writeTargetConfig(cfg)
	require.Error(t, err)

	var msg message.Message
	require.True(t, errors.As(err, &msg))
	require.Equal(t, message.EngineTargetConfigWriteFailure, msg.Code())
	meta := msg.Metadata()
	require.Equal(t, configPath, meta["path"])
}

func TestRemoveTarget(t *testing.T) {
	mlhv := mockLocalhostVerifier{}
	mlhv.On("IsLocalhostSupported").Return(false)
	targetService, testTargetFilepath, sshKeyFile, _ := SetupTargetDirectory(t, &mlhv)

	localhostMlhv := mockLocalhostVerifier{}
	localhostMlhv.On("IsLocalhostSupported").Return(true)
	localhostTargetService, _, _, _ := SetupTargetDirectory(t, &localhostMlhv)

	t.Run("Remove target when filepath does not exist should fail appropriately", func(t *testing.T) {
		err := targetService.RemoveTarget("irrelevant_name")
		assert.ErrorContains(t, err, message.EngineTargetConfigDoesNotExist)
	})

	// Remove target succeeds
	t.Run("Remove target succeeds", func(t *testing.T) {
		_ = targetService.AddTarget("foo", makeSimpleSSHTarget("super-cool-host.com", 22, "tester", sshKeyFile))
		err := targetService.RemoveTarget("foo")
		assert.NoError(t, err)
		assert.Error(t, checkFileContains(testTargetFilepath, "foo"))
	})

	t.Run("Remove Android target succeeds", func(t *testing.T) {
		deviceIP := "android-target.invalid:5555"
		err := targetService.AddTarget("android", &AndroidTarget{SerialNumber: "device-123", DeviceIPAddress: &deviceIP})
		require.NoError(t, err)

		err = targetService.RemoveTarget("android")
		assert.NoError(t, err)

		_, err = targetService.GetTarget("android")
		assert.ErrorContains(t, err, message.EngineTargetConfigDoesNotExist)
		assert.Error(t, checkFileContains(testTargetFilepath, `"serial_number": "device-123"`))
	})

	// Remove all targets works
	t.Run("Remove all targets succeeds", func(t *testing.T) {
		_ = targetService.AddTarget("foo", makeSimpleSSHTarget("super-cool-host.com", 22, "tester", sshKeyFile))
		_ = targetService.AddTarget("baz", makeSimpleSSHTarget("super-cool-host.com", 22, "tester", sshKeyFile))
		err := targetService.RemoveAllTargets()
		assert.NoError(t, err)
		// check targets no longer exist
		assert.Error(t, checkFileContains(testTargetFilepath, "foo"))
		assert.Error(t, checkFileContains(testTargetFilepath, "baz"))
	})

	t.Run("Removing localhost fails", func(t *testing.T) {
		err := localhostTargetService.RemoveTarget(LocalhostName)
		assert.ErrorContains(t, err, message.EngineTargetConfigCannotRemoveReserved)
	})
}

func TestSetDefaultTarget(t *testing.T) {
	mlhv := mockLocalhostVerifier{}
	mlhv.On("IsLocalhostSupported").Return(false)
	targetService, _, _, _ := SetupTargetDirectory(t, &mlhv)
	t.Run("Set active target fails if target does not exist", func(t *testing.T) {
		err := targetService.SetDefaultTarget("beep boop")
		expectedErr := message.New(message.EngineTargetConfigDoesNotExist).WithMetadata(map[string]string{"name": "beep boop"})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("Set Android target as default succeeds", func(t *testing.T) {
		deviceIP := "android-target.invalid:5555"
		err := targetService.AddTarget("android", &AndroidTarget{SerialNumber: "device-123", DeviceIPAddress: &deviceIP})
		require.NoError(t, err)

		err = targetService.SetDefaultTarget("android")
		assert.NoError(t, err)

		defaultTargetName, err := targetService.GetDefaultTargetName()
		assert.NoError(t, err)
		assert.Equal(t, "android", defaultTargetName)

		defaultTarget, err := targetService.GetDefaultTarget()
		assert.NoError(t, err)
		androidTarget, ok := defaultTarget.(*AndroidTarget)
		require.True(t, ok)
		assert.Equal(t, "device-123", androidTarget.SerialNumber)
	})
}

func TestGetTarget(t *testing.T) {
	mlhv := mockLocalhostVerifier{}
	mlhv.On("IsLocalhostSupported").Return(false)
	targetService, _, sshKeyFile, _ := SetupTargetDirectory(t, &mlhv)

	localhostMlhv := mockLocalhostVerifier{}
	localhostMlhv.On("IsLocalhostSupported").Return(true)
	localhostTargetService, _, _, _ := SetupTargetDirectory(t, &localhostMlhv)

	t.Run("Get target succeeds", func(t *testing.T) {
		_ = targetService.AddTarget("foo", makeSimpleSSHTarget("111.111.111.111", 22, "tester", sshKeyFile))
		target, err := targetService.GetTarget("foo")
		assert.NoError(t, err)
		assert.Equal(t, "111.111.111.111", target.(*SSHTarget).Jumps[0].Host)
	})

	t.Run("Get target fails when no target exists", func(t *testing.T) {
		_, err := targetService.GetTarget("not_a_real_target")
		assert.ErrorContains(t, err, message.EngineTargetConfigDoesNotExist)
	})

	t.Run("Get localhost target succeeds", func(t *testing.T) {
		_, err := localhostTargetService.GetTarget(LocalhostName)
		assert.NoError(t, err)
	})

	t.Run("Get localhost target succeeds after removing all targets", func(t *testing.T) {
		_ = localhostTargetService.AddTarget("foo", makeSimpleSSHTarget("super-cool-host.com", 22, "tester", sshKeyFile))
		_ = localhostTargetService.AddTarget("baz", makeSimpleSSHTarget("super-cool-host.com", 22, "tester", sshKeyFile))
		err := localhostTargetService.RemoveAllTargets()
		assert.NoError(t, err)

		_, err = localhostTargetService.GetTarget(LocalhostName)
		assert.NoError(t, err)
	})

}

func TestGetDefaultDevice(t *testing.T) {
	mlhv := mockLocalhostVerifier{}
	mlhv.On("IsLocalhostSupported").Return(false)
	targetService, _, sshKeyFile, _ := SetupTargetDirectory(t, &mlhv)

	localhostMlhv := mockLocalhostVerifier{}
	localhostMlhv.On("IsLocalhostSupported").Return(true)
	localhostTargetService, _, _, _ := SetupTargetDirectory(t, &localhostMlhv)

	t.Run("Get default target fails when no default is set", func(t *testing.T) {
		_, err := targetService.GetDefaultTarget()
		assert.ErrorContains(t, err, message.EngineTargetConfigMissingDefault)
	})

	t.Run("Get default target succeeds after adding first device", func(t *testing.T) {
		_ = targetService.AddTarget("foo", makeSimpleSSHTarget("111.111.111.111", 22, "tester", sshKeyFile))
		target, err := targetService.GetDefaultTarget()
		assert.NoError(t, err)
		assert.Equal(t, "111.111.111.111", target.(*SSHTarget).Jumps[0].Host)
	})

	t.Run("Get localhost default target succeeds", func(t *testing.T) {
		target, err := localhostTargetService.GetDefaultTarget()
		assert.NoError(t, err)
		assert.Equal(t, LocalhostName, target.DisplayHost())
	})
}

func TestGetDefaultTargetName(t *testing.T) {
	mlhv := mockLocalhostVerifier{}
	mlhv.On("IsLocalhostSupported").Return(false)
	targetService, _, sshKeyFile, _ := SetupTargetDirectory(t, &mlhv)

	localhostMlhv := mockLocalhostVerifier{}
	localhostMlhv.On("IsLocalhostSupported").Return(true)
	localhostTargetService, _, _, _ := SetupTargetDirectory(t, &localhostMlhv)

	t.Run("Get default target name succeeds after adding first device", func(t *testing.T) {
		_ = targetService.AddTarget("foobar-fighters", makeSimpleSSHTarget("111.111.111.111", 22, "tester", sshKeyFile))
		targetName, err := targetService.GetDefaultTargetName()
		assert.NoError(t, err)
		assert.Equal(t, "foobar-fighters", targetName)
	})

	t.Run("Get localhost default target name succeeds", func(t *testing.T) {
		target, err := localhostTargetService.GetDefaultTarget()
		assert.NoError(t, err)
		assert.Equal(t, LocalhostName, target.DisplayHost())
	})
}

func TestUpdateTarget(t *testing.T) {
	mlhv := mockLocalhostVerifier{}
	mlhv.On("IsLocalhostSupported").Return(false)
	targetService, _, sshKeyFile, _ := SetupTargetDirectory(t, &mlhv)

	_ = targetService.AddTarget("foobar-fighters", makeSimpleSSHTarget("111.111.111.111", 22, "tester", sshKeyFile))
	_ = targetService.AddTarget("test", makeSimpleSSHTarget("111.111.111.111", 22, "tester", sshKeyFile))
	_ = targetService.AddTarget("same_name", makeSimpleSSHTarget("111.111.111.111", 22, "tester", sshKeyFile))
	_ = targetService.AddTarget("my_jump_target", makeExampleJumpSSHTarget(sshKeyFile))

	t.Run("Update target fails if non-existing target is given", func(t *testing.T) {
		err := targetService.UpdateTarget("idontexist", &UpdateTargetFields{
			Name:          "apap!",
			DefaultFlag:   true,
			UpdatedTarget: &SSHTarget{},
		})
		assert.ErrorContains(t, err, message.EngineTargetConfigDoesNotExist)
	})

	t.Run("Update target succeeds with all valid parameters", func(t *testing.T) {
		err := targetService.UpdateTarget("foobar-fighters", &UpdateTargetFields{
			Name:        "apap!",
			DefaultFlag: true,
			UpdatedTarget: &SSHTarget{Jumps: []SSHHostConfig{
				{Host: "10.252.252.252", Port: 2002, Username: "elliot_woz_ere", PrivateKeyFilename: sshKeyFile},
			}},
		})
		assert.NoError(t, err)

		// Check old target doesn't exist
		_, err = targetService.GetTarget("foobar-fighters")
		assert.Error(t, err)

		// Check new target exists and is default
		target, err := targetService.GetDefaultTarget()
		assert.NoError(t, err)
		defaultName, err := targetService.GetDefaultTargetName()
		assert.NoError(t, err)

		assert.Equal(t, "apap!", defaultName)
		assert.IsType(t, &SSHTarget{}, target)
		sshTarget := target.(*SSHTarget)
		assert.Equal(t, 1, len(sshTarget.Jumps))
		assert.Equal(t, "10.252.252.252", sshTarget.Jumps[0].Host)
		assert.Equal(t, int32(2002), sshTarget.Jumps[0].Port)
		assert.Equal(t, "elliot_woz_ere", sshTarget.Jumps[0].Username)
		assert.Equal(t, sshKeyFile, sshTarget.Jumps[0].PrivateKeyFilename)

	})

	t.Run("Update target correctly validates relevant fields", func(t *testing.T) {
		err := targetService.UpdateTarget("test", &UpdateTargetFields{
			Name:        LocalhostName,
			DefaultFlag: false,
			UpdatedTarget: &SSHTarget{Jumps: []SSHHostConfig{
				{Host: "111.111.111.111", Port: 22, Username: "", PrivateKeyFilename: sshKeyFile},
			}},
		})
		assert.ErrorContains(t, err, message.EngineTargetConfigNameReserved)

		err = targetService.UpdateTarget("test", &UpdateTargetFields{
			Name:        "",
			DefaultFlag: false,
			UpdatedTarget: &SSHTarget{Jumps: []SSHHostConfig{
				{Host: "not@an$$#][IP]", Port: 22, Username: "", PrivateKeyFilename: sshKeyFile},
			}},
		})
		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EngineTargetConfigInvalidHostFormat, msgErr.Code())
		assert.Equal(t, "not@an$$#][IP]", msgErr.Metadata()["hostAddress"])
		assert.Equal(t, "<unknown>@not@an$$#][IP]", msgErr.Metadata()["jumpNode"])

		err = targetService.UpdateTarget("test", &UpdateTargetFields{
			Name:        "",
			DefaultFlag: false,
			UpdatedTarget: &SSHTarget{Jumps: []SSHHostConfig{
				{Host: "111.111.111.111", Port: -400, Username: "", PrivateKeyFilename: sshKeyFile},
			}},
		})
		ok = errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EngineCommonInvalidPortFormat, msgErr.Code())
		assert.Equal(t, "-400", msgErr.Metadata()["portNum"])
		assert.Equal(t, "<unknown>@111.111.111.111:-400", msgErr.Metadata()["jumpNode"])

		err = targetService.UpdateTarget("test", &UpdateTargetFields{
			Name:        "",
			DefaultFlag: false,
			UpdatedTarget: &SSHTarget{Jumps: []SSHHostConfig{
				{Host: "111.111.111.111", Port: 22, Username: "", PrivateKeyFilename: "/super/real/path(*&^*&(&*^))"},
			}},
		})
		assert.ErrorContains(t, err, message.EngineSshKeyFileNotFound)
	})

	t.Run("Update target preserves the default target after updating", func(t *testing.T) {
		err := targetService.SetDefaultTarget("test")
		assert.NoError(t, err)

		// Update target but do not specify set default
		err = targetService.UpdateTarget("test", &UpdateTargetFields{
			Name:        "is_still_default?",
			DefaultFlag: false,
			UpdatedTarget: &SSHTarget{Jumps: []SSHHostConfig{
				{Host: "111.111.111.111", Port: 22, Username: "", PrivateKeyFilename: ""},
			}},
		})

		assert.NoError(t, err)
		defaultTargetName, err := targetService.GetDefaultTargetName()
		assert.NoError(t, err)
		assert.Equal(t, "is_still_default?", defaultTargetName)

	})

	t.Run("Update target with the exact same configuration succeeds", func(t *testing.T) {
		err := targetService.UpdateTarget("same_name", &UpdateTargetFields{
			Name:        "same_name",
			DefaultFlag: false,
			UpdatedTarget: &SSHTarget{Jumps: []SSHHostConfig{
				{Host: "111.111.111", Port: 22, Username: "tester", PrivateKeyFilename: sshKeyFile},
			}},
		})
		assert.NoError(t, err)
	})

	t.Run("Update target with jump hosts succeeds with all valid parameters", func(t *testing.T) {
		newJumps := []SSHHostConfig{
			{Host: "11.11.11.11", Port: 11, Username: "Hop11", PrivateKeyFilename: sshKeyFile},
			{Host: "22.22.22.22", Port: 22, Username: "Hop22", PrivateKeyFilename: sshKeyFile},
			{Host: "33.33.33.33", Port: 33, Username: "NewFinalHop", PrivateKeyFilename: sshKeyFile},
		}

		err := targetService.UpdateTarget("my_jump_target", &UpdateTargetFields{
			Name:          "",
			DefaultFlag:   false,
			UpdatedTarget: &SSHTarget{Jumps: newJumps},
		})
		assert.NoError(t, err)

		updatedTarget, err := targetService.GetTarget("my_jump_target")
		assert.NoError(t, err)
		assert.IsType(t, &SSHTarget{}, updatedTarget)
		updatedSSHTarget := updatedTarget.(*SSHTarget)

		assert.Equal(t, len(newJumps), len(updatedSSHTarget.Jumps))

		for i := range len(newJumps) - 1 {
			assert.Equal(t, newJumps[i].Host, updatedSSHTarget.Jumps[i].Host)
			assert.Equal(t, newJumps[i].Port, updatedSSHTarget.Jumps[i].Port)
			assert.Equal(t, newJumps[i].PrivateKeyFilename, updatedSSHTarget.Jumps[i].PrivateKeyFilename)
			assert.Equal(t, newJumps[i].Username, updatedSSHTarget.Jumps[i].Username)
		}
	})

}

func TestTryUnmarshalJSONTargetString(t *testing.T) {
	t.Run("successfully unmarshals JSON target string", func(t *testing.T) {
		jsonString := `{
			"type": "ssh",
			"jumps": [
				{
					"username": "jumpuser",
					"host": "jump.example.com",
					"port": 22,
					"auth": {
						"method": "private_key",
						"private_key_path": "/path/to/jump_key"
					},
					"host_key_policy": "accept_new"
				}
			],
			"username": "targetuser",
			"host": "target.example.com",
			"port": 22,
			"auth": {
				"method": "private_key",
				"private_key_path": "/path/to/target_key"
			},
			"host_key_policy": "strict"
		}`

		target, err := TryUnmarshalJSONTargetString(jsonString)

		assert.NoError(t, err)
		assert.NotNil(t, target)
	})

	t.Run("returns nil for non-JSON string", func(t *testing.T) {
		nonJSON := "just_a_regular_string"
		target, err := TryUnmarshalJSONTargetString(nonJSON)

		assert.NoError(t, err)
		assert.Nil(t, target)
	})

}

func TestReadTargetConfig(t *testing.T) {
	t.Run("handles empty target config file as no targets", func(t *testing.T) {
		verifier := mockLocalhostVerifier{}
		verifier.On("IsLocalhostSupported").Return(false)

		configPath := filepath.Join(t.TempDir(), "targets.json")
		require.NoError(t, os.WriteFile(configPath, nil, perms.LocalFilePerm))

		manager := NewTargetManager(configPath, &verifier)
		config, err := manager.ReadTargetConfig()
		require.NoError(t, err)
		assert.Empty(t, config.Targets)
		assert.Empty(t, config.Default)
	})

	t.Run("handles whitespace-only target config file as no targets", func(t *testing.T) {
		verifier := mockLocalhostVerifier{}
		verifier.On("IsLocalhostSupported").Return(false)

		configPath := filepath.Join(t.TempDir(), "targets.json")
		require.NoError(t, os.WriteFile(configPath, []byte(" \n\t"), perms.LocalFilePerm))

		manager := NewTargetManager(configPath, &verifier)
		config, err := manager.ReadTargetConfig()
		require.NoError(t, err)
		assert.Empty(t, config.Targets)
		assert.Empty(t, config.Default)
	})

	t.Run("fails if target config file cannot be created", func(t *testing.T) {
		metadata := map[string]string{"path": "/fake/path", "parentDir": "/fake/parent_dir"}
		expectedMsg := message.New(message.EngineTargetConfigCreateFailure).WithMetadata(metadata)

		mtm := MockTargetManager{}
		mtm.On("ReadTargetConfig", mock.Anything, mock.Anything).Return(TargetConfig{}, expectedMsg)

		_, err := mtm.ReadTargetConfig()
		assert.ErrorContains(t, err, message.EngineTargetConfigCreateFailure)
		assert.Equal(t, expectedMsg, err)
	})

	t.Run("fails if localhost already exists", func(t *testing.T) {
		metadata := map[string]string{"path": "/fake/path"}
		expectedMsg := message.New(message.EngineTargetConfigLocalhostAlreadyExists).WithMetadata(metadata)

		mtm := MockTargetManager{}
		mtm.On("ReadTargetConfig", mock.Anything, mock.Anything).Return(TargetConfig{}, expectedMsg)

		_, err := mtm.ReadTargetConfig()
		assert.ErrorContains(t, err, message.EngineTargetConfigLocalhostAlreadyExists)
		assert.Equal(t, expectedMsg, err)
	})

	t.Run("fails if target config file cannot be read", func(t *testing.T) {
		metadata := map[string]string{"path": "/fake/path"}
		expectedMsg := message.New(message.EngineTargetConfigReadFailure).WithMetadata(metadata)

		mtm := MockTargetManager{}
		mtm.On("ReadTargetConfig", mock.Anything, mock.Anything).Return(TargetConfig{}, expectedMsg)

		_, err := mtm.ReadTargetConfig()
		assert.ErrorContains(t, err, message.EngineTargetConfigReadFailure)
		assert.Equal(t, expectedMsg, err)
	})

	t.Run("fails if target config file cannot be parsed", func(t *testing.T) {
		metadata := map[string]string{"path": "/fake/path"}
		expectedMsg := message.New(message.EngineTargetConfigParseFailure).WithMetadata(metadata)

		mtm := MockTargetManager{}
		mtm.On("ReadTargetConfig", mock.Anything, mock.Anything).Return(TargetConfig{}, expectedMsg)

		_, err := mtm.ReadTargetConfig()
		assert.ErrorContains(t, err, message.EngineTargetConfigParseFailure)
		assert.Equal(t, expectedMsg, err)
	})

	t.Run("fails if a target in the config file cannot be parsed", func(t *testing.T) {
		metadata := map[string]string{"path": "/fake/path", "target": "bad_target"}
		expectedMsg := message.New(message.EngineTargetConfigParseTargetFailure).WithMetadata(metadata)

		mtm := MockTargetManager{}
		mtm.On("ReadTargetConfig", mock.Anything, mock.Anything).Return(TargetConfig{}, expectedMsg)

		_, err := mtm.ReadTargetConfig()
		assert.ErrorContains(t, err, message.EngineTargetConfigParseTargetFailure)
		assert.Equal(t, expectedMsg, err)
	})
}
