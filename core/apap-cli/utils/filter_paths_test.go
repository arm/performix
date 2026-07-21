// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"reflect"
	"strings"
	"testing"
)

func TestStringsSplitColonSeparated(t *testing.T) {
	tests := []struct {
		input            string
		expectedSplit    []string
		expectedFiltered []string
	}{
		{"a:b:c", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"a::b", []string{"a", "", "b"}, []string{"a", "b"}},
		{":a:b:", []string{"", "a", "b", ""}, []string{"a", "b"}},
		{"", []string{""}, []string{}},
		{"a", []string{"a"}, []string{"a"}},
	}

	for _, tt := range tests {
		result := strings.Split(tt.input, ":")
		if !reflect.DeepEqual(result, tt.expectedSplit) {
			t.Errorf("Split(%q) = %v; want %v", tt.input, result, tt.expectedSplit)
		}
		filtered := FilterPaths(result)
		if !reflect.DeepEqual(filtered, tt.expectedFiltered) {
			t.Errorf("FilterPaths(Split(%q)) = %v; want %v", tt.input, filtered, tt.expectedFiltered)
		}
	}
}

func TestFilterPaths(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{"NoEmptyStrings", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"SomeEmptyStrings", []string{"a", "", "b", "", "c"}, []string{"a", "b", "c"}},
		{"AllEmptyStrings", []string{"", "", ""}, []string{}},
		{"EmptySlice", []string{}, []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterPaths(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("FilterPaths(%v) = %v; want %v", tt.input, result, tt.expected)
			}
		})
	}
}
