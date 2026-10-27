// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// VS Code treats mcp.json as JSON with Comments (JSONC), so its configuration
// may contain comments and trailing commas. These helpers retain the parsed
// syntax tree so install and uninstall can change only the managed server entry
// without rewriting the user's comments or unrelated configuration.
package mcpclientinstaller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/tailscale/hujson"

	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type vscodeJSONCDocument struct {
	// Keep the syntax tree and file mode together because both must survive an
	// atomic replacement of an existing user configuration file.
	value hujson.Value
	mode  fs.FileMode
}

func readVSCodeJSONC(path string) (*vscodeJSONCDocument, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// A missing configuration is an empty object for registration purposes.
		// The file is not created until install has a server entry to add.
		data = []byte(`{}`)
	} else if err != nil {
		return nil, err
	}

	mode := fs.FileMode(0o600)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if len(bytes.TrimSpace(data)) == 0 {
		// VS Code may create mcp.json before it contains any configuration.
		data = []byte(`{}`)
	}

	value, err := hujson.Parse(data)
	if err != nil {
		return nil, err
	}
	if _, ok := value.Value.(*hujson.Object); !ok {
		return nil, fmt.Errorf("MCP client configuration is not a JSON object")
	}
	return &vscodeJSONCDocument{value: value, mode: mode}, nil
}

func decodeVSCodeJSONCValue(value *hujson.Value, target any) error {
	// Standardize a clone so encoding/json can provide the existing typed
	// decoding while the original comments and trailing commas remain intact.
	standard := value.Clone()
	standard.Standardize()
	return json.Unmarshal(standard.Pack(), target)
}

func vscodeJSONCRegistrationState(
	document *vscodeJSONCDocument,
	server ServerDefinition,
) (RegistrationState, []RegistrationDifference, error) {
	// Only the managed entry affects registration state. The surrounding JSONC
	// syntax remains part of the document for any later mutation.
	servers := document.value.Find("/servers")
	if servers == nil || servers.Value.Kind() == 'n' {
		return RegistrationStateNotConfigured, nil, nil
	}
	if _, ok := servers.Value.(*hujson.Object); !ok {
		return RegistrationStateUnreadable, nil, fmt.Errorf(
			"MCP client server configuration is not a JSON object",
		)
	}

	entry := document.value.Find("/servers/" + escapeJSONPointer(server.Name))
	if entry == nil {
		return RegistrationStateNotConfigured, nil, nil
	}
	var configured jsonServerEntry
	if err := decodeVSCodeJSONCValue(entry, &configured); err != nil {
		return RegistrationStateUnreadable, nil, err
	}
	differences := registrationDifferences(
		configured.Type,
		configured.Command,
		configured.Args,
		configured.Env,
		server,
		true,
	)
	if len(differences) == 0 {
		return RegistrationStateConfigured, nil, nil
	}
	return RegistrationStateConflict, differences, nil
}

func readVSCodeJSONCRegistrationState(
	path string,
	server ServerDefinition,
) (RegistrationState, []RegistrationDifference, error) {
	document, err := readVSCodeJSONC(path)
	if err != nil {
		return RegistrationStateUnreadable, nil, err
	}
	return vscodeJSONCRegistrationState(document, server)
}

type jsonPatchOperation struct {
	Operation string          `json:"op"`
	Path      string          `json:"path"`
	Value     json.RawMessage `json:"value,omitempty"`
}

func installVSCodeJSONC(path string, server ServerDefinition) error {
	document, err := readVSCodeJSONC(path)
	if err != nil {
		return err
	}
	// Re-check the entry in the document being written. This avoids overwriting
	// a conflicting entry if mcp.json changed after the manager's initial check.
	state, _, err := vscodeJSONCRegistrationState(document, server)
	if err != nil {
		return err
	}
	switch state {
	case RegistrationStateConfigured:
		return nil
	case RegistrationStateConflict:
		return fmt.Errorf("MCP server %q has a conflicting configuration", server.Name)
	}

	entry, err := json.Marshal(jsonServerEntry{
		Type:    "stdio",
		Command: server.Command,
		Args:    append([]string{}, server.Args...),
	})
	if err != nil {
		return err
	}
	serverPath := "/servers/" + escapeJSONPointer(server.Name)
	// A syntax-tree patch changes only the managed member, preserving comments,
	// trailing commas, formatting, and unrelated server definitions.
	operation := jsonPatchOperation{Operation: "add", Path: serverPath, Value: entry}
	servers := document.value.Find("/servers")
	if servers == nil || servers.Value.Kind() == 'n' {
		// JSON Patch cannot add a child below a missing or null parent, so create
		// the complete servers object in that case.
		serverMap, marshalErr := json.Marshal(map[string]json.RawMessage{server.Name: entry})
		if marshalErr != nil {
			return marshalErr
		}
		operation = jsonPatchOperation{Operation: "add", Path: "/servers", Value: serverMap}
	}
	if err := patchVSCodeJSONC(&document.value, operation); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return util.WriteFileAtomic(path, document.value.Pack(), document.mode)
}

func uninstallVSCodeJSONC(path string, server ServerDefinition) error {
	document, err := readVSCodeJSONC(path)
	if err != nil {
		return err
	}
	// Re-check before deletion so a same-named entry changed by the user is not
	// mistaken for the registration managed by Arm Performix.
	state, _, err := vscodeJSONCRegistrationState(document, server)
	if err != nil {
		return err
	}
	switch state {
	case RegistrationStateNotConfigured:
		return nil
	case RegistrationStateConflict:
		return fmt.Errorf("MCP server %q has a conflicting configuration", server.Name)
	}

	// Removing the managed member also removes comments attached to that member;
	// comments belonging to the surrounding document remain in its syntax tree.
	operation := jsonPatchOperation{
		Operation: "remove",
		Path:      "/servers/" + escapeJSONPointer(server.Name),
	}
	if err := patchVSCodeJSONC(&document.value, operation); err != nil {
		return err
	}
	return util.WriteFileAtomic(path, document.value.Pack(), document.mode)
}

func patchVSCodeJSONC(document *hujson.Value, operation jsonPatchOperation) error {
	patch, err := json.Marshal([]jsonPatchOperation{operation})
	if err != nil {
		return err
	}
	return document.Patch(patch)
}

func escapeJSONPointer(value string) string {
	// Server names form part of an RFC 6901 path and may contain its two special
	// characters even though the current Arm Performix name does not.
	value = strings.ReplaceAll(value, "~", "~0")
	return strings.ReplaceAll(value, "/", "~1")
}
