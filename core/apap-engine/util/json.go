// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mitchellh/mapstructure"
)

func DecodeJSON[T any](data []byte) (*T, error) {
	var result T
	err := json.Unmarshal(data, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// DecodeJSONWithHook decodes JSON data into a struct of type T using a custom decode hook.
func DecodeJSONWithHook[T any](data []byte, hook mapstructure.DecodeHookFunc) (*T, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	var result T
	decoderConfig := &mapstructure.DecoderConfig{
		DecodeHook: hook,
		Result:     &result,
		TagName:    "json",
	}
	decoder, err := mapstructure.NewDecoder(decoderConfig)
	if err != nil {
		return nil, err
	}
	if err := decoder.Decode(raw); err != nil {
		return nil, err
	}
	return &result, nil
}

func EncodeJSON[T any](data *T) ([]byte, error) {
	result, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func StructToMap[T any](data *T) (map[string]any, error) {
	var result map[string]any

	bytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(bytes, &result)
	return result, err
}

func ReadJSONFile[T any](filename string) (*T, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		var empty T
		return &empty, nil
	}

	return DecodeJSON[T](data)
}

func WriteJSONFile[T any](filename string, data *T, perm fs.FileMode) error {
	result, err := EncodeJSON(data)
	if err != nil {
		return err
	}

	return os.WriteFile(filename, result, perm)
}

// WriteJSONFileAtomic writes JSON by replacing the target file with a fully written
// temp file from the same directory. Readers see either the old file or the new file,
// never a truncated in-place write. The final replacement is platform-specific
// because Windows does not support overwriting an existing file with os.Rename.
func WriteJSONFileAtomic[T any](filename string, data *T, perm fs.FileMode) error {
	result, err := EncodeJSON(data)
	if err != nil {
		return err
	}

	dir := filepath.Dir(filename)
	tmpFile, err := os.CreateTemp(dir, "."+filepath.Base(filename)+".*.tmp")
	if err != nil {
		return err
	}

	tmpName := tmpFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmpFile.Chmod(perm); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if _, err := tmpFile.Write(result); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := atomicReplaceFile(tmpName, filename); err != nil {
		return err
	}

	cleanup = false
	return nil
}

// ReadJSONFileWithHook reads a JSON file and decodes it into the target struct using a custom decode hook.
func ReadJSONFileWithHook[T any](filename string, hook mapstructure.DecodeHookFunc) (*T, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	return DecodeJSONWithHook[T](data, hook)
}

// IsJSON checks if the input string looks like a valid JSON object or array.
func IsJSON(s string) bool {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) == 0 {
		return false
	}

	// First, check it's syntactically valid JSON
	if !json.Valid([]byte(trimmed)) {
		return false
	}

	// Then restrict to object or array
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}
