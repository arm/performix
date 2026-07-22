// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsJson(t *testing.T) {
	t.Run("successfully identifies valid JSON", func(t *testing.T) {
		validJSON := `{"name": "test", "age": 30}`
		if !IsJSON(validJSON) {
			t.Errorf("Expected valid JSON")
		}
	})

	t.Run("successfully identifies invalid JSON", func(t *testing.T) {
		invalidJSON := `{"name": "test", "age": 30`
		if IsJSON(invalidJSON) {
			t.Errorf("Expected invalid JSON")
		}
	})
	t.Run("successfully identifies valid target string as valid JSON", func(t *testing.T) {
		validTargetJSON := `{
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
		if !IsJSON(validTargetJSON) {
			t.Errorf("Expected valid target JSON")
		}
	})
	t.Run("successfully identifies invalid target string as invalid JSON", func(t *testing.T) {
		// The following JSON is invalid due to a missing closing brace
		invalidTargetJSON := `{
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
		` // Missing closing brace here
		if IsJSON(invalidTargetJSON) {
			t.Errorf("Expected invalid target JSON")
		}
	})
	t.Run("successfully identifies empty string as invalid JSON", func(t *testing.T) {
		emptyString := `   `
		if IsJSON(emptyString) {
			t.Errorf("Expected empty string to be invalid JSON")
		}
	})
	t.Run("successfully identifies JSON array", func(t *testing.T) {
		jsonArray := `["item1", "item2", "item3"]`
		if !IsJSON(jsonArray) {
			t.Errorf("Expected valid JSON array")
		}
	})
	t.Run("successfully identifies non-JSON string", func(t *testing.T) {
		nonJSON := `Just a regular string`
		if IsJSON(nonJSON) {
			t.Errorf("Expected non-JSON string to be invalid JSON")
		}
	})
	t.Run("successfully identifies JSON with leading/trailing whitespace", func(t *testing.T) {
		jsonWithWhitespace := `   { "key": "value" }   `
		if !IsJSON(jsonWithWhitespace) {
			t.Errorf("Expected valid JSON with whitespace")
		}
	})
	t.Run("successfully identifies nested JSON objects", func(t *testing.T) {
		nestedJSON := `{"outer": {"inner": {"key": "value"}}}`
		if !IsJSON(nestedJSON) {
			t.Errorf("Expected valid nested JSON")
		}
	})
	t.Run("successfully identifies JSON with special characters", func(t *testing.T) {
		jsonWithSpecialChars := `{"text": "Hello, world! @#$%^&*()"}`
		if !IsJSON(jsonWithSpecialChars) {
			t.Errorf("Expected valid JSON with special characters")
		}
	})
	t.Run("successfully rejects strings only containing whitespace", func(t *testing.T) {
		whitespaceOnly := "     \n\t   "
		if IsJSON(whitespaceOnly) {
			t.Errorf("Expected whitespace-only string to be invalid JSON")
		}
	})
}

func TestWriteJSONFileAtomic(t *testing.T) {
	t.Run("replaces existing file and cleans up temp file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "manifest.json")
		if err := os.WriteFile(path, []byte(`{"old":true}`), 0o600); err != nil {
			t.Fatalf("write old file: %v", err)
		}

		data := map[string]bool{"new": true}
		if err := WriteJSONFileAtomic(path, &data, 0o600); err != nil {
			t.Fatalf("write atomic: %v", err)
		}

		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read written file: %v", err)
		}
		if string(got) != "{\"new\":true}" {
			t.Fatalf("unexpected file contents: %s", got)
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read dir: %v", err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".manifest.json.") && strings.HasSuffix(entry.Name(), ".tmp") {
				t.Fatalf("atomic temp file was not cleaned up: %s", entry.Name())
			}
		}
	})

	t.Run("preserves existing file if json encoding fails", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "manifest.json")
		const oldContents = `{"old":true}`
		if err := os.WriteFile(path, []byte(oldContents), 0o600); err != nil {
			t.Fatalf("write old file: %v", err)
		}

		// encoding/json cannot marshal channels, so this forces an encoding error
		// before WriteJSONFileAtomic creates or renames a temp file.
		data := map[string]chan int{"invalid": make(chan int)}
		err := WriteJSONFileAtomic(path, &data, 0o600)
		if err == nil {
			t.Fatal("expected encoding error")
		}

		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read preserved file: %v", readErr)
		}
		if string(got) != oldContents {
			t.Fatalf("existing file was changed: got %s want %s", got, oldContents)
		}
	})
}
