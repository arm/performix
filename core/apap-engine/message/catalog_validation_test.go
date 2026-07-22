// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package message

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// any lowercase char, apostrophe, any lowercase char except s (to not match things like "the recipe's parameters")
// if match is found, the string is invalid
var noContractionsRegex = regexp.MustCompile(`[a-z]['’][a-rt-z]`)

// opening brace, any letter or number (or nothing), invalid char, any letter or number (or nothing), closing brace
// if match is found, the string is invalid
var disallowedPlaceholderCharsRegex = regexp.MustCompile(fmt.Sprintf(`\{%v*[^a-zA-Z0-9]%v*\}`, PlaceholderCharsRegex, PlaceholderCharsRegex))

// e.g. (and variations), i.e. (and variations), etc, via
// if match is found, the string is invalid
var egRegex1 = regexp.MustCompile(`[^a-zA-Z]eg[\s\.]`)
var egRegex2 = regexp.MustCompile(`e\.g`)
var ieRegex1 = regexp.MustCompile(`[^a-zA-Z]ie[\s\.]`)
var ieRegex2 = regexp.MustCompile(`i\.e`)
var etcRegex = regexp.MustCompile(`[^a-zA-Z]etc[^a-zA-Z]`)
var viaRegex = regexp.MustCompile(`[^a-zA-Z]via[^a-zA-Z]`)

// '{something}` or `{something}'
// if match is found, the string is invalid
var mismatchRegex1 = regexp.MustCompile(fmt.Sprintf(`%v\{%v*\}'`, "`", PlaceholderCharsRegex))
var mismatchRegex2 = regexp.MustCompile(fmt.Sprintf(`'\{%v*\}%v`, PlaceholderCharsRegex, "`"))

func matchAnyRegex(s string, regexes []*regexp.Regexp) bool {
	for _, regex := range regexes {
		if regex.MatchString(s) {
			return true
		}
	}
	return false
}

// validNonConsecBraces verifies that there are no mismatched braces in the string (opening brace without matching
// closing brace, two consecutive opening braces).
func validNonConsecBraces(s string) bool {
	openBrace := false
	for _, char := range s {
		switch char {
		case '{':
			if openBrace {
				return false
			}
			openBrace = true
		case '}':
			if !openBrace {
				return false
			}
			openBrace = false
		}
	}
	return !openBrace
}

// noUnknownPluralities verifies that the string doesn't contain any instances of "(s)" (e.g. "{numFiles} file(s)")
// This isn't allowed as it doesn't translate easily into other languages.
func noUnknownPluralities(s string) bool {
	return !strings.Contains(s, "(s)")
}

// noContractions verifies that the string doesn't contain any contractions (e.g. "don't" instead of "do not", "you're"
// instead of "you are"). These can obfuscate meaning and make translation more difficult.
func noContractions(s string) bool {
	return !noContractionsRegex.MatchString(s)
}

// noDisallowedPlaceholderChars verifies that placeholder fields in the string don't contain any disallowed characters.
// Placeholders should only contain uppercase or lowercase letters, or numbers.
func noDisallowedPlaceholderChars(s string) bool {
	return !disallowedPlaceholderCharsRegex.MatchString(s)
}

// noLatinWords verifies that the string doesn't contain any latin words or abbreviations ("e.g.", "i.e.", "etc", "via")
func noLatinWords(s string) bool {
	disallowed := []*regexp.Regexp{
		egRegex1,
		egRegex2,
		ieRegex1,
		ieRegex2,
		etcRegex,
		viaRegex,
	}
	return !matchAnyRegex(s, disallowed)
}

// noBritishSpellings verifies that the string doesn't contain any British English spellings of words which differ from
// the American version.
func noBritishSpellings(s string) bool {
	disallowed := []string{
		"yse",
	}
	for _, str := range disallowed {
		if strings.Contains(s, str) {
			return false
		}
	}
	return true
}

// noMismatchedQuoteAndGraves verifies that placeholders are either enclosed in single quotes, graves, or not enclosed
// at all - that is, they cannot have a single quote at the start and a grave at the end, or vice versa.
func noMismatchedQuoteAndGraves(s string) bool {
	disallowed := []*regexp.Regexp{
		mismatchRegex1,
		mismatchRegex2,
	}
	return !matchAnyRegex(s, disallowed)
}

func validateField(t *testing.T, value string, field string, messageCode string) {
	assert.True(t, validNonConsecBraces(value), "Invalid pattern of braces in '%v' field of '%v'", field, messageCode)
	assert.True(t, noUnknownPluralities(value), "'(s)' in '%v' field of '%v'; if plurality is unknown, two separate messages should be created", field, messageCode)
	assert.True(t, noContractions(value), "'%v' field '%v' uses a contraction (e.g. \"don't\"); non-contracted version (e.g. \"do not\") should be used instead.", field, messageCode)
	assert.True(t, noDisallowedPlaceholderChars(value), "Disallowed character in placeholder in '%v' field of '%v'; only letters and numbers are allowed", field, messageCode)
	assert.True(t, noLatinWords(value), "Latin word/abbreviation (e.g, i.e, etc, via) found in '%v' field of '%v'; the technical writing guide suggests these should not be used.", field, messageCode)
	assert.True(t, noBritishSpellings(value), "British spelling used in '%v' field of '%v' (e.g. -yse vs -yze); ensure American version is used instead")
	assert.True(t, noMismatchedQuoteAndGraves(value), "Placeholder in '%v' field of '%v' has grave at start and single quote at end (or vice versa)")
}

func TestValidateCatalog(t *testing.T) {
	t.Run("validate fields", func(t *testing.T) {
		data, err := os.ReadFile(CatalogFile)
		assert.NoError(t, err)

		var nested map[string]any
		err = json.Unmarshal(data, &nested)
		assert.NoError(t, err)

		fields := map[string]struct{}{
			"Message":     {},
			"Explanation": {},
			"Advice":      {},
		}

		// Recursive function to validate all leaf entries
		var validate func(path string, node any)
		validate = func(path string, node any) {
			obj, ok := node.(map[string]any)
			if !ok {
				return
			}

			// If this is a leaf node with a "Message" field, validate it
			if _, isLeaf := obj["Message"]; isLeaf {
				for key := range fields {
					value, _ := obj[key].(string)
					validateField(t, value, key, path)
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

// TestValidateTests verifies that the tests used above for validation purposes complain in invalid situations, and
// don't complain in valid situations.
func TestValidateTests(t *testing.T) {
	t.Run("validNonConsecBraces works as intended", func(t *testing.T) {
		testCases := []struct {
			str      string
			expected bool
		}{
			{
				"abc {def} ghi",
				true,
			},
			{
				"{abc {def} ghi}",
				false,
			},
			{
				"abc {def} {ghi",
				false,
			},
			{
				"{}ab{c }{def} ghi{}{}",
				true,
			},
		}
		for _, test := range testCases {
			assert.Equal(t, test.expected, validNonConsecBraces(test.str), fmt.Sprintf("string: %v", test.str))
		}
	})
	t.Run("noUnknownPluralities works as intended", func(t *testing.T) {
		testCases := []struct {
			str      string
			expected bool
		}{
			{
				"3 bike(s)",
				false,
			},
			{
				"3 bikes",
				true,
			},
			{
				"1 bike",
				true,
			},
			{
				"a bike (as)",
				true,
			},
		}
		for _, test := range testCases {
			assert.Equal(t, test.expected, noUnknownPluralities(test.str), fmt.Sprintf("string: %v", test.str))
		}
	})
	t.Run("noContractions works as intended", func(t *testing.T) {
		testCases := []struct {
			str      string
			expected bool
		}{
			{
				"didn't",
				false,
			},
			{
				"would've",
				false,
			},
			{
				"you're",
				false,
			},
			{
				"you’re",
				false,
			},
			{
				"this file's name is rubbish",
				true,
			},
			// Ideally this wouldn't pass, but "'s" can be valid in some situations, like above
			{
				"it's simple",
				true,
			},
		}
		for _, test := range testCases {
			assert.Equal(t, test.expected, noContractions(test.str), fmt.Sprintf("string: %v", test.str))
		}
	})
	t.Run("noDisallowedPlaceholderChars works as intended", func(t *testing.T) {
		testCases := []struct {
			str      string
			expected bool
		}{
			{
				"a {bcdEfg456}[` ",
				true,
			},
			{
				"a `{asd`}`",
				false,
			},
			{
				"this {is_a} test",
				false,
			},
			{
				"abc {def} {GHIj9%k}",
				false,
			},
			// Valid as {} is not treated as a placeholder, just two symbols
			{
				"a {} adsd",
				true,
			},
		}
		for _, test := range testCases {
			assert.Equal(t, test.expected, noDisallowedPlaceholderChars(test.str), fmt.Sprintf("string: %v", test.str))
		}
	})
	t.Run("noLatinWords works as intended", func(t *testing.T) {
		testCases := []struct {
			str      string
			expected bool
		}{
			{
				"creepy, i.e. scary",
				false,
			},
			{
				"creepy,i.e scary",
				false,
			},
			{
				"creepy(ie scary)",
				false,
			},
			{
				"creperie (scary)",
				true,
			},
			{
				"abc e.g. def",
				false,
			},
			{
				"abc eg. def",
				false,
			},
			{
				"abc (eggs) def",
				true,
			},
			{
				"(e.g.15)",
				false,
			},
			{
				"(eg.15)",
				false,
			},
			{
				"abceg. 15)",
				true,
			},
			{
				"bob eg 15",
				false,
			},
			{
				"keg 15",
				true,
			},
			{
				"a,b,c,etc.",
				false,
			},
			{
				"a,b,c, etc ",
				false,
			},
			{
				"a,b,c, etch-a-sketch",
				true,
			},
			{
				"this via that",
				false,
			},
			{
				"this 'via that'",
				false,
			},
			{
				"this aviary",
				true,
			},
		}
		for _, test := range testCases {
			assert.Equal(t, test.expected, noLatinWords(test.str), fmt.Sprintf("string: %v", test.str))
		}
	})
	t.Run("noBritishSpellings works as intended", func(t *testing.T) {
		testCases := []struct {
			str      string
			expected bool
		}{
			{
				"analyze",
				true,
			},
			{
				"analyse",
				false,
			},
			{
				"your",
				true,
			},
			{
				"mediocre",
				true,
			},
			{
				"demise",
				true,
			},
			{
				"improvisation",
				true,
			},
		}
		for _, test := range testCases {
			assert.Equal(t, test.expected, noBritishSpellings(test.str), fmt.Sprintf("string: %v", test.str))
		}
	})
	t.Run("noMismatchedQuoteAndGraves works as intended", func(t *testing.T) {
		testCases := []struct {
			str      string
			expected bool
		}{
			{
				"`{abc}'",
				false,
			},
			{
				"'{def}`",
				false,
			},
			{
				"'{hij}",
				true,
			},
			{
				"{klm}`",
				true,
			},
			{
				"{nop}",
				true,
			},
		}
		for _, test := range testCases {
			assert.Equal(t, test.expected, noMismatchedQuoteAndGraves(test.str), fmt.Sprintf("string: %v", test.str))
		}
	})
}
