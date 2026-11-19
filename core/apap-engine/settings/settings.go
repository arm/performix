// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// The settings package supports system-wide user settings, shared between GUI, CLI, and MCP server.
package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const Filename = "settings.json"
const ProfilingFocus = "profilingFocus"

// Use a separate, persistent lock file because saving replaces the settings file.
func acquireLock(ctx context.Context, path string, shared bool) (*flock.Flock, error) {
	lock := flock.New(path+".lock", flock.SetPermissions(perms.LocalFilePerm))
	var err error
	if shared {
		_, err = lock.TryRLockContext(ctx, 10*time.Millisecond)
	} else {
		_, err = lock.TryLockContext(ctx, 10*time.Millisecond)
	}
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		releaseLock(lock)
		return nil, err
	}
	return lock, nil
}

func releaseLock(lock *flock.Flock) {
	if err := lock.Unlock(); err != nil {
		log.WithError(err).Warn("Failed to release settings lock")
	}
}

func validateName(name string) error {
	if name != ProfilingFocus {
		return message.New(message.EngineSettingsInvalidName).WithMetadata(map[string]string{"name": name})
	}
	return nil
}

func validateValue(name string, value any) error {
	switch name {
	case ProfilingFocus:
		if focus, ok := value.(string); ok && (focus == "mobile" || focus == "server" || focus == "none") {
			return nil
		}
	}
	return message.New(message.EngineSettingsInvalidValue).WithMetadata(map[string]string{"name": name})
}

func readFile(path string) (map[string]json.RawMessage, error) {
	// #nosec G703 -- The path comes from engine configuration; setting names are never used as paths.
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return make(map[string]json.RawMessage), nil
	}
	if err != nil {
		return nil, message.New(message.EngineSettingsReadFailed).WithMetadata(map[string]string{"path": path}).WithCause(err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return make(map[string]json.RawMessage), nil
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, message.New(message.EngineSettingsParseFailed).WithMetadata(map[string]string{"path": path}).WithCause(err)
	}
	if config == nil {
		return nil, message.New(message.EngineSettingsParseFailed).WithMetadata(map[string]string{"path": path})
	}
	return config, nil
}

// Read returns a saved setting, or nil if it has not been set.
func Read(ctx context.Context, path, name string) (any, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A missing configuration directory means no settings have been saved yet.
	// #nosec G703 -- The path comes from engine configuration; setting names are never used as paths.
	if _, err := os.Stat(filepath.Dir(path)); os.IsNotExist(err) {
		return nil, nil
	}
	lock, err := acquireLock(ctx, path, true)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, message.New(message.EngineSettingsReadFailed).WithMetadata(map[string]string{"path": path}).WithCause(err)
	}
	defer releaseLock(lock)
	config, err := readFile(path)
	if err != nil {
		return nil, err
	}
	raw, exists := config[name]
	if !exists {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, message.New(message.EngineSettingsParseFailed).WithMetadata(map[string]string{"path": path}).WithCause(err)
	}
	if err := validateValue(name, value); err != nil {
		return nil, message.New(message.EngineSettingsParseFailed).WithMetadata(map[string]string{"path": path}).WithCause(err)
	}
	return value, nil
}

// Write validates and saves a setting, keeping other saved settings unchanged.
// A nil value removes the setting from the file.
func Write(ctx context.Context, path, name string, value any) error {
	if err := validateName(name); err != nil {
		return err
	}
	if value != nil {
		if err := validateValue(name, value); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// #nosec G703 -- The path comes from engine configuration; setting names are never used as paths.
	if err := os.MkdirAll(filepath.Dir(path), perms.LocalDirPerm); err != nil {
		return message.New(message.EngineSettingsWriteFailed).WithMetadata(map[string]string{"path": path}).WithCause(err)
	}
	lock, err := acquireLock(ctx, path, false)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return message.New(message.EngineSettingsWriteFailed).WithMetadata(map[string]string{"path": path}).WithCause(err)
	}
	defer releaseLock(lock)

	config, err := readFile(path)
	if err != nil {
		return err
	}
	if value == nil {
		delete(config, name)
	} else {
		raw, err := json.Marshal(value)
		if err != nil {
			return message.New(message.EngineSettingsWriteFailed).WithMetadata(map[string]string{"path": path}).WithCause(err)
		}
		config[name] = raw
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return message.New(message.EngineSettingsWriteFailed).WithMetadata(map[string]string{"path": path}).WithCause(err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := util.WriteFileAtomic(path, data, perms.LocalFilePerm); err != nil {
		return message.New(message.EngineSettingsWriteFailed).WithMetadata(map[string]string{"path": path}).WithCause(err)
	}
	return nil
}
