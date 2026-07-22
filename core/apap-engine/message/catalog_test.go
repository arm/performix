// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package message

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	CatalogFile    = "catalog_en-US.json"
	headerFileName = "copyright-license-header.txt"
)

func TestCatalog(t *testing.T) {
	expectedMetadata := copyrightLicenseMetadata(t)

	t.Run("catalog is flattened correctly", func(t *testing.T) {
		raw := map[string]any{
			"metadata": map[string]any{
				"copyright":   expectedMetadata["copyright"],
				"license":     expectedMetadata["license"],
				"description": "Arm Performix message catalog for en-US locale",
			},
			"engine": map[string]any{
				"recipe": map[string]any{
					"run": map[string]any{
						"ERROR_ONE": map[string]any{
							"Severity":    "Error",
							"Message":     "Something went wrong.",
							"Explanation": "The daemon was not running.",
							"Advice":      "Start the daemon and try again.",
						},
					},
				},
			},
		}

		out := make(map[MessageCode]CatalogMessage)
		flattenCatalog("", raw, out)

		msg, ok := out["engine.recipe.run.ERROR_ONE"]
		assert.True(t, ok)
		_, hasMetadataMessage := out["metadata"]
		assert.False(t, hasMetadataMessage)
		assert.Equal(t, "Error", string(msg.Severity))
		assert.Equal(t, "Something went wrong.", msg.Message)
		assert.Equal(t, "The daemon was not running.", msg.Explanation)
		assert.Equal(t, "Start the daemon and try again.", msg.Advice)
	})
	t.Run("catalog metadata keys are detected", func(t *testing.T) {
		assert.True(t, IsCatalogMetadataKey("metadata"))
		assert.True(t, IsCatalogMetadataKey("_schema"))
		assert.False(t, IsCatalogMetadataKey("engine"))
		assert.False(t, IsCatalogMetadataKey("metadataExtra"))
	})
	t.Run("as string always produces a string", func(t *testing.T) {
		assert.Equal(t, "hello", asString("hello"))
		assert.Equal(t, "", asString(123))
	})
	t.Run("catalog en-US is valid JSON", func(t *testing.T) {
		data, err := os.ReadFile(CatalogFile)
		assert.NoError(t, err)

		var v map[string]any
		err = json.Unmarshal(data, &v)
		assert.NoErrorf(t, err, "%s is not valid JSON", CatalogFile)
	})
	t.Run("catalog en-US has copyright and license metadata", func(t *testing.T) {
		data, err := os.ReadFile(CatalogFile)
		assert.NoError(t, err)

		var catalog map[string]any
		err = json.Unmarshal(data, &catalog)
		assert.NoError(t, err)

		metadata, ok := catalog["metadata"].(map[string]any)
		assert.True(t, ok)
		assert.Equal(t, expectedMetadata["copyright"], metadata["copyright"])
		assert.Equal(t, expectedMetadata["license"], metadata["license"])
	})
	t.Run("test all catalog entries have required fields", func(t *testing.T) {
		data, err := os.ReadFile(CatalogFile)
		assert.NoError(t, err)

		var nested map[string]any
		err = json.Unmarshal(data, &nested)
		assert.NoError(t, err)

		// Recursive function to validate all leaf entries
		var validate func(path string, node any)
		validate = func(path string, node any) {
			obj, ok := node.(map[string]any)
			if !ok {
				return
			}

			// If this is a leaf node with a "Message" field, validate it
			if _, isLeaf := obj["Message"]; isLeaf {

				// Build the required keys based on the severity
				var requiredKeys map[string]struct{}

				if obj["Severity"] == "Info" {
					requiredKeys = map[string]struct{}{
						"Severity": {},
						"Message":  {},
					}
				} else {
					requiredKeys = map[string]struct{}{
						"Severity":    {},
						"Message":     {},
						"Explanation": {},
						"Advice":      {},
					}
				}

				for key := range requiredKeys {
					_, exists := obj[key]
					assert.True(t, exists, "Missing required field %q in message: %s", key, path)
				}

				for key := range obj {
					_, allowed := requiredKeys[key]
					assert.True(t, allowed, "Unexpected extra field %q in message: %s", key, path)
				}
			} else {
				// Recurse into nested structure
				for k, v := range obj {
					childPath := k
					if path != "" {
						childPath = path + "." + k
					}
					validate(childPath, v)
				}
			}
		}

		validate("", nested)
	})
}

func copyrightLicenseMetadata(t *testing.T) map[string]string {
	t.Helper()

	header, err := os.ReadFile(filepath.Join(repoRoot(t), headerFileName))
	assert.NoError(t, err)

	metadata := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(header)), "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), ":")
		if !found {
			continue
		}
		switch key {
		case "SPDX-FileCopyrightText":
			metadata["copyright"] = strings.TrimSpace(value)
		case "SPDX-License-Identifier":
			metadata["license"] = strings.TrimSpace(value)
		}
	}

	assert.NotEmpty(t, metadata["copyright"])
	assert.NotEmpty(t, metadata["license"])
	return metadata
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, sourceFile, _, ok := runtime.Caller(0)
	assert.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", ".."))
}

func TestInterpolate(t *testing.T) {
	t.Run("interpolation works across all fields", func(t *testing.T) {
		oldMsg := &CatalogMessage{
			Severity:    SeverityError,
			Message:     "People with the name {name} are cool",
			Explanation: "That's because {name} is a great name",
			Advice:      "Change your name to {name} and you can be cool like {name} too",
		}
		metadata := map[string]string{"name": "James"}

		newMsg := oldMsg.Interpolate(metadata)

		expectedMsg := &CatalogMessage{
			Severity:    SeverityError,
			Message:     "People with the name James are cool",
			Explanation: "That's because James is a great name",
			Advice:      "Change your name to James and you can be cool like James too",
		}

		assert.Equal(t, newMsg, expectedMsg)
	})
	t.Run("nothing interpolated when placeholder missing", func(t *testing.T) {
		oldMsg := &CatalogMessage{
			Severity:    SeverityError,
			Message:     "People with the name James are cool",
			Explanation: "That's because James is a great name",
			Advice:      "Change your name to James and you can be cool like James too",
		}
		metadata := map[string]string{"name": "Owen"}

		newMsg := oldMsg.Interpolate(metadata)
		assert.Equal(t, newMsg, oldMsg)
	})
	t.Run("placeholders without provided metadata values are not interpolated", func(t *testing.T) {
		oldMsg := &CatalogMessage{
			Severity:    SeverityError,
			Message:     "People with the name {name} are cool",
			Explanation: "That's because {name} is a great name",
			Advice:      "Change your name to {name} and you can be cool like {name} too",
		}
		metadata := map[string]string{"abc": "def"}

		newMsg := oldMsg.Interpolate(metadata)
		assert.Equal(t, newMsg, oldMsg)
	})
	t.Run("placeholders are interpolated with empty values", func(t *testing.T) {
		oldMsg := &CatalogMessage{
			Severity:    SeverityError,
			Message:     "People with the name {name} are cool",
			Explanation: "That's because {name} is a great name",
			Advice:      "Change your name to {name} and you can be cool like {name} too",
		}
		metadata := map[string]string{"name": ""}

		newMsg := oldMsg.Interpolate(metadata)

		expectedMessage := &CatalogMessage{
			Severity:    SeverityError,
			Message:     "People with the name  are cool",
			Explanation: "That's because  is a great name",
			Advice:      "Change your name to  and you can be cool like  too",
		}
		assert.Equal(t, expectedMessage, newMsg)
	})
	t.Run("{} is not a placeholder", func(t *testing.T) {
		oldMsg := &CatalogMessage{
			Severity:    SeverityError,
			Message:     "People with the name {} are cool",
			Explanation: "That's because {} is a great name",
			Advice:      "Change your name to {} and you can be cool like {} too",
		}
		metadata := map[string]string{"": "don't replace me!"}

		newMsg := oldMsg.Interpolate(metadata)

		assert.Equal(t, oldMsg, newMsg)
	})
	t.Run("catalog constants are interpolated", func(t *testing.T) {
		oldMsg := &CatalogMessage{
			Severity:    SeverityError,
			Message:     "People with the name {$PRODUCT_FULL_NAME} are cool",
			Explanation: "That's because {$PRODUCT_FULL_NAME} is a great name",
			Advice:      "Change your name to {$PRODUCT_FULL_NAME} and you can be cool too",
		}
		newMsg := oldMsg.Interpolate(map[string]string{})

		expectedMessage := &CatalogMessage{
			Severity:    SeverityError,
			Message:     fmt.Sprintf("People with the name %v are cool", catalogConstants["PRODUCT_FULL_NAME"]),
			Explanation: fmt.Sprintf("That's because %v is a great name", catalogConstants["PRODUCT_FULL_NAME"]),
			Advice:      fmt.Sprintf("Change your name to %v and you can be cool too", catalogConstants["PRODUCT_FULL_NAME"]),
		}
		assert.Equal(t, expectedMessage, newMsg)
	})
	t.Run("metadata keys starting with a $ are ignored", func(t *testing.T) {
		oldMsg := &CatalogMessage{
			Severity:    SeverityError,
			Message:     "People with the name {$ABC} are cool",
			Explanation: "That's because {$ABC} is a great name",
			Advice:      "Change your name to {$ABC} and you can be cool too",
		}
		newMsg := oldMsg.Interpolate(map[string]string{"$ABC": "123"})
		assert.Equal(t, oldMsg, newMsg)
	})
	t.Run("metadata keys can match constant keys (excluding $)", func(t *testing.T) {
		oldMsg := &CatalogMessage{
			Severity:    SeverityError,
			Message:     "People with the name {$PRODUCT_FULL_NAME} are cool",
			Explanation: "That's because {PRODUCT_FULL_NAME} is a great name",
			Advice:      "Change your name to {$PRODUCT_FULL_NAME} and you can be cool too",
		}
		newMsg := oldMsg.Interpolate(map[string]string{"PRODUCT_FULL_NAME": "123"})

		expectedMessage := &CatalogMessage{
			Severity:    SeverityError,
			Message:     fmt.Sprintf("People with the name %v are cool", catalogConstants["PRODUCT_FULL_NAME"]),
			Explanation: "That's because 123 is a great name",
			Advice:      fmt.Sprintf("Change your name to %v and you can be cool too", catalogConstants["PRODUCT_FULL_NAME"]),
		}
		assert.Equal(t, expectedMessage, newMsg)
	})
}

func TestString(t *testing.T) {
	t.Run("string representation of an info message includes severity and message", func(t *testing.T) {
		msg := &CatalogMessage{
			Severity: SeverityInfo,
			Message:  "Information is cool",
		}

		str := msg.String()
		expectedContents := []string{msg.Severity, msg.Message}
		for _, ec := range expectedContents {
			assert.Contains(t, str, ec)
		}
	})

	t.Run("string representation of a warning message includes severity, message, explanation and advice", func(t *testing.T) {
		msg := &CatalogMessage{
			Severity:    SeverityWarning,
			Message:     "This is a warning",
			Explanation: "Something might be wrong, but it's not blocking.",
			Advice:      "Try it anyway and see what happens.",
		}

		str := msg.String()
		expectedContents := []string{msg.Severity, "Explanation", "Advice", msg.Message, msg.Explanation, msg.Advice}
		for _, ec := range expectedContents {
			assert.Contains(t, str, ec)
		}
	})

	t.Run("string representation of an error message includes severity, message, explanation, advice and code", func(t *testing.T) {
		msg := &CatalogMessage{
			Code:        "my.made_up.ERROR_CODE",
			Severity:    SeverityError,
			Message:     "This is an error",
			Explanation: "Something has gone wrong.",
			Advice:      "Follow this advice and try again.",
		}

		str := msg.String()
		expectedContents := []string{msg.Severity, "Explanation", "Advice", "Code", msg.Message, msg.Explanation, msg.Advice, msg.Code}
		for _, ec := range expectedContents {
			assert.Contains(t, str, ec)
		}
	})

	t.Run("0 indent", func(t *testing.T) {
		msg := &CatalogMessage{
			Severity: SeverityInfo,
			Message:  "Information is cool",
		}

		str := msg.String()
		assert.Equal(t, fmt.Sprintf("[%v]: Information is cool", SeverityInfo), str)
	})
}

func TestStringWithIndent(t *testing.T) {
	t.Run("indent is applied correctly", func(t *testing.T) {
		msg := &CatalogMessage{
			Code:        "my.made_up.ERROR_CODE",
			Severity:    SeverityError,
			Message:     "This is an error",
			Explanation: "Something has gone wrong.",
			Advice:      "Follow this advice and try again.",
		}

		str := msg.StringWithIndent(3)
		expectedContents := []string{
			fmt.Sprintf("   [%v]: %v", msg.Severity, msg.Message),
			fmt.Sprintf("   [Explanation]: %v", msg.Explanation),
			fmt.Sprintf("   [Advice]: %v", msg.Advice),
			fmt.Sprintf("   [Code]: %v", msg.Code),
		}
		for _, ec := range expectedContents {
			assert.Contains(t, str, ec)
		}
	})
}

func TestCatalogConstants(t *testing.T) {
	t.Run("all catalog constants use SCREAMING_SNAKE_CASE", func(t *testing.T) {
		screamingSnakeCaseRegex := regexp.MustCompile("^[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)*$")
		for key := range catalogConstants {
			assert.True(t, screamingSnakeCaseRegex.Match([]byte(key)), fmt.Sprintf("Catalog constant '%v' is not SCREAMING_SNAKE_CASE", key))
		}
	})
}
