// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package semver

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseSemVer_Valid(t *testing.T) {
	tests := []struct {
		in         string
		wantMajor  int
		wantMinor  int
		wantPatch  int
		wantString string
	}{
		{"1.0", 1, 0, 0, "1.0.0"},
		{"2.5", 2, 5, 0, "2.5.0"},
		{"3.4.1", 3, 4, 1, "3.4.1"},
		{"10.20.0", 10, 20, 0, "10.20.0"},
		{"1.0.0-alpha", 1, 0, 0, "1.0.0"},
		{"1.2.3-beta.1", 1, 2, 3, "1.2.3"},
		{"1.2.3+build", 1, 2, 3, "1.2.3"},
		{"1.2.3-alpha+build", 1, 2, 3, "1.2.3"},
		{"1.2.3-alpha.1+21AF26D3", 1, 2, 3, "1.2.3"},
		{"1.0.0+21AF26D3----117B344092BD", 1, 0, 0, "1.0.0"},
	}
	for _, tc := range tests {
		v, err := ParseSemVer(tc.in)
		if err != nil {
			t.Fatalf("ParseSemVer(%q) unexpected error: %v", tc.in, err)
		}
		if v.Major != tc.wantMajor || v.Minor != tc.wantMinor || v.Patch != tc.wantPatch {
			t.Fatalf("ParseSemVer(%q) = %+v; want {%d,%d,%d}", tc.in, v, tc.wantMajor, tc.wantMinor, tc.wantPatch)
		}
		if got := v.String(); got != tc.wantString {
			t.Fatalf("(%s).String() = %q; want %q", tc.in, got, tc.wantString)
		}
	}
}

func TestParseSemVer_Invalid(t *testing.T) {
	bads := []string{
		"", "1", "1.", ".1", "1.2.", "1.2.3.4", "a.b", "1.b.3", "-1.0", "1.-1.0",
		"-1.0.0", "1.0.0-", "1.0.0+", "1.0.0--alpha", "1.0.0++build",
		"1.0.0-alpha+build+extra",
		"1.0.0+build+extra",
		"1.0.0-alpha-beta",
	}
	for _, in := range bads {
		if _, err := ParseSemVer(in); err == nil {
			t.Fatalf("ParseSemVer(%q) = nil error; want error", in)
		}
	}
}

func TestSemVerString(t *testing.T) {
	v1 := SemVer{Major: 1, Minor: 2, Patch: 0}
	if got := v1.String(); got != "1.2.0" {
		t.Fatalf("(1.2.0).String() = %q, want %q", got, "1.2.0")
	}
	v2 := SemVer{Major: 1, Minor: 2, Patch: 3}
	if got := v2.String(); got != "1.2.3" {
		t.Fatalf("(1.2.3).String() = %q, want %q", got, "1.2.3")
	}
}

func TestCmp(t *testing.T) {
	cases := []struct {
		a, b  SemVer
		want  int
		label string
	}{
		{SemVer{1, 0, 0}, SemVer{1, 0, 0}, 0, "eq"},
		{SemVer{1, 0, 0}, SemVer{1, 0, 1}, -1, "patch lt"},
		{SemVer{1, 2, 0}, SemVer{1, 1, 9}, 1, "minor gt"},
		{SemVer{2, 0, 0}, SemVer{1, 99, 99}, 1, "major gt"},
	}
	for _, tc := range cases {
		if got := Cmp(tc.a, tc.b); got != tc.want {
			t.Fatalf("Cmp(%v, %v) = %d; want %d (%s)", tc.a, tc.b, got, tc.want, tc.label)
		}
	}
}

func TestInRange(t *testing.T) {
	// [1.0.0, 2.0.0)
	r := VersionRange{
		Min: &SemVer{1, 0, 0},
		Max: &SemVer{2, 0, 0},
	}
	must := func(s string) SemVer {
		v, err := ParseSemVer(s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return v
	}
	if !InRange(must("1.0.0"), r) {
		t.Fatal("1.0.0 should be in range [1.0.0, 2.0.0)")
	}
	if !InRange(must("1.9.9"), r) {
		t.Fatal("1.9.9 should be in range [1.0.0, 2.0.0)")
	}
	if InRange(must("2.0.0"), r) {
		t.Fatal("2.0.0 should NOT be in range [1.0.0, 2.0.0)")
	}

	// Min only: [1.1.0, +∞)
	r2 := VersionRange{Min: &SemVer{1, 1, 0}}
	if !InRange(must("1.1.0"), r2) || !InRange(must("9.9.9"), r2) {
		t.Fatal("expected >= 1.1.0 to be in range")
	}
	if InRange(must("1.0.9"), r2) {
		t.Fatal("1.0.9 should NOT satisfy >= 1.1.0")
	}

	// Max only: (-∞, 1.5.0)
	r3 := VersionRange{Max: &SemVer{1, 5, 0}}
	if !InRange(must("0.9.9"), r3) || !InRange(must("1.4.9"), r3) {
		t.Fatal("expected < 1.5.0 to be in range")
	}
	if InRange(must("1.5.0"), r3) {
		t.Fatal("1.5.0 should NOT satisfy < 1.5.0")
	}

	// [1.1.0, 2.0.0] - inclusive max
	r4 := VersionRange{
		Min:          &SemVer{1, 0, 0},
		Max:          &SemVer{2, 0, 0},
		InclusiveMax: true,
	}
	if !InRange(must("1.0.0"), r4) {
		t.Fatal("1.0.0 should be in range [1.0.0, 2.0.0]")
	}
	if !InRange(must("1.9.9"), r4) {
		t.Fatal("1.9.9 should be in range [1.0.0, 2.0.0]")
	}
	if !InRange(must("2.0.0"), r4) {
		t.Fatal("2.0.0 should be in range [1.0.0, 2.0.0]")
	}
}

func TestIntersection(t *testing.T) {
	testCases := []struct {
		name     string
		ranges   []VersionRange
		expected *VersionRange
	}{
		{
			name:     "no ranges",
			ranges:   []VersionRange{},
			expected: nil,
		},
		{
			name:     "single range",
			ranges:   []VersionRange{{Min: &SemVer{1, 0, 0}}},
			expected: &VersionRange{Min: &SemVer{1, 0, 0}},
		},
		{
			name:     "empty ranges",
			ranges:   []VersionRange{{}, {}},
			expected: nil,
		},
		{
			name: "two ranges",
			ranges: []VersionRange{
				{Min: &SemVer{1, 0, 0}, Max: &SemVer{3, 0, 0}},
				{Min: &SemVer{0, 7, 0}, Max: &SemVer{2, 8, 9}},
			},
			expected: &VersionRange{Min: &SemVer{1, 0, 0}, Max: &SemVer{2, 8, 9}},
		},
		{
			name: "three ranges",
			ranges: []VersionRange{
				{Min: &SemVer{1, 0, 0}, Max: &SemVer{3, 0, 0}},
				{Min: &SemVer{0, 7, 0}, Max: &SemVer{2, 8, 9}},
				{Min: &SemVer{2, 1, 0}, Max: &SemVer{4, 2, 3}},
			},
			expected: &VersionRange{Min: &SemVer{2, 1, 0}, Max: &SemVer{2, 8, 9}},
		},
		{
			name: "no intersection",
			ranges: []VersionRange{
				{Min: &SemVer{1, 0, 0}, Max: &SemVer{3, 0, 0}},
				{Min: &SemVer{0, 7, 0}, Max: &SemVer{2, 8, 9}},
				{Min: &SemVer{3, 1, 0}, Max: &SemVer{4, 2, 3}},
			},
			expected: &VersionRange{},
		},
		{
			name: "three ranges, range constraining max has InclusiveMax = true",
			ranges: []VersionRange{
				{Min: &SemVer{1, 0, 0}, Max: &SemVer{3, 0, 0}},
				{Min: &SemVer{0, 7, 0}, Max: &SemVer{2, 8, 9}, InclusiveMax: true},
				{Min: &SemVer{2, 1, 0}, Max: &SemVer{4, 2, 3}},
			},
			expected: &VersionRange{Min: &SemVer{2, 1, 0}, Max: &SemVer{2, 8, 9}, InclusiveMax: true},
		},
	}
	for _, testCase := range testCases {
		result := Intersection(testCase.ranges...)
		assert.Equal(t, testCase.expected, result, fmt.Sprintf("'%v' failed", testCase.name))
	}
}

func TestFindDisjointRanges(t *testing.T) {
	testCases := []struct {
		name           string
		ranges         []VersionRange
		expectedIndex1 int
		expectedIndex2 int
	}{
		{
			name:           "no ranges",
			ranges:         []VersionRange{},
			expectedIndex1: -1,
			expectedIndex2: -1,
		},
		{
			name:           "empty ranges",
			ranges:         []VersionRange{{}, {}},
			expectedIndex1: -1,
			expectedIndex2: -1,
		},
		{
			name:           "single range",
			ranges:         []VersionRange{{Min: &SemVer{1, 0, 0}}},
			expectedIndex1: -1,
			expectedIndex2: -1,
		},
		{
			name:           "empty range and non-empty range",
			ranges:         []VersionRange{{}, {Min: &SemVer{1, 0, 0}}},
			expectedIndex1: -1,
			expectedIndex2: -1,
		},
		{
			name: "valid intersection",
			ranges: []VersionRange{
				{Min: &SemVer{0, 0, 0}, Max: &SemVer{5, 0, 0}},
				{Min: &SemVer{0, 1, 0}, Max: &SemVer{5, 0, 0}},
				{Min: &SemVer{1, 0, 0}, Max: &SemVer{4, 0, 0}},
				{Min: &SemVer{3, 0, 0}, Max: &SemVer{3, 0, 0}},
			},
			expectedIndex1: -1,
			expectedIndex2: -1,
		},
		{
			name: "valid intersection, no max",
			ranges: []VersionRange{
				{Min: &SemVer{0, 0, 0}},
				{Min: &SemVer{0, 1, 0}},
			},
			expectedIndex1: -1,
			expectedIndex2: -1,
		},
		{
			name: "0 and 2 mutually exclusive",
			ranges: []VersionRange{
				{Min: &SemVer{0, 0, 0}, Max: &SemVer{3, 0, 0}},
				{Min: &SemVer{1, 0, 0}, Max: &SemVer{4, 0, 0}},
				{Min: &SemVer{3, 0, 1}, Max: &SemVer{3, 0, 8}},
			},
			expectedIndex1: 0,
			expectedIndex2: 2,
		},
		{
			name: "0 and 3 mutually exclusive, with empty range",
			ranges: []VersionRange{
				{Min: &SemVer{0, 0, 0}, Max: &SemVer{3, 0, 0}},
				{Min: &SemVer{1, 0, 0}, Max: &SemVer{4, 0, 0}},
				{},
				{Min: &SemVer{3, 0, 1}, Max: &SemVer{3, 0, 8}},
			},
			expectedIndex1: 0,
			expectedIndex2: 3,
		},
		{
			name: "0 and 2 mutually exclusive, with empty range between",
			ranges: []VersionRange{
				{Min: &SemVer{0, 0, 0}, Max: &SemVer{3, 0, 0}},
				{},
				{Min: &SemVer{5, 0, 0}, Max: &SemVer{5, 0, 0}},
			},
			expectedIndex1: 0,
			expectedIndex2: 2,
		},
		{
			name: "0 and 2, and 1 and 2, mutually exclusive",
			ranges: []VersionRange{
				{Min: &SemVer{0, 0, 0}, Max: &SemVer{3, 0, 0}},
				{Min: &SemVer{1, 0, 0}, Max: &SemVer{2, 0, 0}},
				{Min: &SemVer{5, 0, 0}, Max: &SemVer{5, 0, 8}},
			},
			expectedIndex1: 1,
			expectedIndex2: 2,
		},
	}
	for _, testCase := range testCases {
		index1, index2 := FindDisjointRanges(testCase.ranges)
		assert.Equal(t, testCase.expectedIndex1, index1, fmt.Sprintf("'%v' index 1 is incorrect", testCase.name))
		assert.Equal(t, testCase.expectedIndex2, index2, fmt.Sprintf("'%v' index 2 is incorrect", testCase.name))
	}
}
