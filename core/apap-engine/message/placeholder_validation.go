// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package message

import (
	"fmt"
	"regexp"
	"strings"
)

var PlaceholderCharsRegex = `[a-zA-Z0-9]`
var placeholderPattern = regexp.MustCompile(fmt.Sprintf(`\{(%v+)\}`, PlaceholderCharsRegex))

// ValidateMetadataPlaceholders verifies that every placeholder defined in the catalog entry for the provided
// error has a corresponding, non-empty value in the message metadata. Returns nil when all placeholders are
// satisfied. Unused metadata entries are ignored.
func ValidateMetadataPlaceholders(err error) error {
	msg, lookupErr := LookupMessage(err)
	if lookupErr != nil || msg.Code == unknownCatalogMessage.Code {
		// Should only error if placeholders are missing
		return nil
	}

	unsetPlaceholders := collectUnsetPlaceholders(msg)
	if len(unsetPlaceholders) == 0 {
		return nil
	}
	unsetString := strings.Join(copyKeysSlice(unsetPlaceholders), ", ")
	return fmt.Errorf("message '%v' has the following unset placeholders: %v", msg.Code, unsetString)
}

func collectUnsetPlaceholders(msg *CatalogMessage) map[string]struct{} {
	// Map used to avoid duplicates
	unsetPlaceholders := map[string]struct{}{}
	for _, field := range []string{
		msg.Message,
		msg.Explanation,
		msg.Advice,
	} {
		matches := placeholderPattern.FindAllStringSubmatch(field, -1)
		for _, match := range matches {
			unsetPlaceholders[match[1]] = struct{}{}
		}
	}
	return unsetPlaceholders
}

func copyKeysSlice[K comparable, V any](m map[K]V) []K {
	cpy := make([]K, 0, len(m))
	for k := range m {
		cpy = append(cpy, k)
	}
	return cpy
}
