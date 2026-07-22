// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package message

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/message/messageutil"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

// To add a new catalog for a new locale:
//
// 1. Create a new JSON file in the message directory. The file should be named
// catalog_<locale>.json, where <locale> is the locale code (e.g. en-US, fr-FR,
// etc.). The JSON file should have the same structure as catalog_en-US.json.
//
// 2. Define a new byte slice to hold the embedded catalog data, using the
// //go:embed directive.
//
// 3. Add a new locale constant for the new locale.

//go:embed catalog_en-US.json
var catalog_EN_US []byte

const (
	LocaleEnglish = "en-US"
)

// catalogs holds the embedded message catalogs for different locales. This is
// a temporary staging area for the catalog contents before they are flattened
// and stored in CatalogByLocale.
var catalogs = map[string][]byte{
	LocaleEnglish: catalog_EN_US,
}

// CatalogByLocale holds the flattened message catalog for each locale. This is
// used by the message package to look up messages by code and locale.
var CatalogByLocale = map[string]map[MessageCode]CatalogMessage{}

// Severity represents the severity level of a CatalogMessage.
type Severity = string

const (
	SeverityInfo    Severity = "Info"
	SeverityWarning Severity = "Warning"
	SeverityError   Severity = "Error"
)

// catalogConstants defines a list of constants that can be referenced in the message
// catalog. Constants are referenced by using the following syntax: {$CONSTANT_KEY}.
var catalogConstants = map[string]string{"PRODUCT_FULL_NAME": terminology.GetProductFullName()}

// CatalogMessage is a structured message obtained from the message catalog.
type CatalogMessage struct {
	Code        MessageCode // Unique code for the message
	Severity    Severity    // Severity of the message (Info, Warning, Error)
	Message     string      // Human-readable, one-line message displayed to the user
	Explanation string      // A detailed explanation of what caused the problem
	Advice      string      // A suggestion of next steps the user should perform to resolve the problem
}

// String formats the CatalogMessage into a human-readable string.
//   - For Info messages, include the Severity and Message fields.
//   - For Warning messages, include the Severity, Message, Explanation, and Advice fields.
//   - For Error messages, include the Severity, Message, Explanation, Advice, and Code fields.
func (c *CatalogMessage) String() string {
	return c.StringWithIndent(0)
}

// StringWithIndent formats the CatalogMessage into a human-readable string, with the specified
// number of spaces before each line.
//   - For Info messages, include the Severity and Message fields.
//   - For Warning messages, include the Severity, Message, Explanation, and Advice fields.
//   - For Error messages, include the Severity, Message, Explanation, Advice, and Code fields.
func (c *CatalogMessage) StringWithIndent(numChars int) string {
	var str string
	indent := strings.Repeat(" ", numChars)
	switch c.Severity {
	case SeverityInfo:
		str = fmt.Sprintf("%s[%s]: %s",
			indent,
			c.Severity,
			c.Message)
	case SeverityWarning:
		str = fmt.Sprintf("%s[%s]: %s\n%s[Explanation]: %s\n%s[Advice]: %s",
			indent,
			c.Severity,
			c.Message,
			indent,
			c.Explanation,
			indent,
			c.Advice)
	case SeverityError:
		str = fmt.Sprintf("%s[%s]: %s\n%s[Explanation]: %s\n%s[Advice]: %s\n%s[Code]: %s",
			indent,
			c.Severity,
			c.Message,
			indent,
			c.Explanation,
			indent,
			c.Advice,
			indent,
			c.Code)
	}
	return str
}

// Text returns the CatalogMessage as a single-line string, omitting severity
// and code fields. Suitable for compact or user-facing displays where metadata
// is not needed.
func (c *CatalogMessage) Text() string {
	fields := []string{c.Message, c.Explanation, c.Advice}
	var filtered []string
	for _, field := range fields {
		if field != "" {
			filtered = append(filtered, field)
		}
	}

	return strings.Join(filtered, " ")
}

// Interpolate replaces any placeholders in the message with values from the
// supplied metadata, and predefined catalog constants. The metadata is
// expected to contain keys that match the placeholder names in the message,
// formatted as {key}. If a placeholder name does not have a corresponding
// key in the metadata, it will be ignored. Catalog constants can be referenced
// using the syntax {$CONSTANT_NAME}.
func (c *CatalogMessage) Interpolate(metadata map[string]string) *CatalogMessage {
	interpolated := CatalogMessage{
		Code:        c.Code,
		Severity:    c.Severity,
		Message:     c.Message,
		Explanation: c.Explanation,
		Advice:      c.Advice,
	}

	mappings := map[string]string{}
	for key, value := range metadata {
		// Skip empty keys, as {} is not a placeholder
		// Skip keys starting with a $, as this syntax is reserved for catalog constants
		if key == "" || key[0] == '$' {
			continue
		}
		placeholder := fmt.Sprintf("{%s}", key)
		mappings[placeholder] = value
	}
	for key, value := range catalogConstants {
		placeholder := fmt.Sprintf("{$%s}", key)
		mappings[placeholder] = value
	}

	for placeholder, value := range mappings {
		// The same placeholder may exist in multiple fields
		interpolated.Message = strings.ReplaceAll(interpolated.Message, placeholder, value)
		interpolated.Explanation = strings.ReplaceAll(interpolated.Explanation, placeholder, value)
		interpolated.Advice = strings.ReplaceAll(interpolated.Advice, placeholder, value)
	}

	return &interpolated
}

// init initializes the message catalog for each locale. It flattens the nested
// structure into a map[MessageCode]CatalogMessage for easy access by message
// code. Called automatically at import time.
//
// The keys in the map are the message codes, which are flat strings like
// "engine.recipe.run.SOME_ERROR". The values are CatalogMessage structs that
// contain the localized message details.
func init() {
	for locale, raw := range catalogs {
		var nested map[string]any
		if err := json.Unmarshal(raw, &nested); err != nil {
			panic(fmt.Errorf("failed to parse %s catalog: %w", locale, err))
		}
		out := map[MessageCode]CatalogMessage{}
		flattenCatalog("", nested, out)
		CatalogByLocale[locale] = out
	}
}

// flattenCatalog flattens a nested message catalog into flat
// map[MessageCode]CatalogMessage so that it can be easily accessed by message
// code (which are flat strings like engine.recipe.run.SOME_ERROR).
func flattenCatalog(prefix string, node any, out map[MessageCode]CatalogMessage) {
	switch typed := node.(type) {
	case map[string]any:
		for k, v := range typed {
			if messageutil.IsCatalogMetadataKey(k) {
				continue
			}
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			switch val := v.(type) {
			case map[string]any:
				if _, ok := val["Message"]; ok {
					out[MessageCode(key)] = CatalogMessage{
						Code:        MessageCode(key),
						Severity:    Severity(asString(val["Severity"])),
						Message:     asString(val["Message"]),
						Explanation: asString(val["Explanation"]),
						Advice:      asString(val["Advice"]),
					}
				} else {
					flattenCatalog(key, val, out)
				}
			}
		}
	}
}

// IsCatalogMetadataKey reports whether a catalog key is metadata rather than a message namespace.
func IsCatalogMetadataKey(key string) bool {
	return messageutil.IsCatalogMetadataKey(key)
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
