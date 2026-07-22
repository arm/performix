// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package semver

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ------------------------
// SemVer (2- or 3-part)
// ------------------------

// SemVer supports 2- or 3-segment versions: MAJOR.MINOR or MAJOR.MINOR.PATCH.
// Patch defaults to 0 when not specified.
type SemVer struct {
	Major int
	Minor int
	Patch int
}

// UnmarshalJSON supports parsing a quoted semver string into SemVer.
func (v *SemVer) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	parsed, err := ParseSemVer(s)
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}

// ParseSemVer accepts "1.0" and "1.0.1". Returns error for anything else.
func ParseSemVer(s string) (SemVer, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return SemVer{}, fmt.Errorf("empty version string")
	}
	if strings.HasSuffix(s, "-") || strings.HasSuffix(s, "+") {
		return SemVer{}, fmt.Errorf("invalid semver: trailing - or +")
	}
	// Only one + allowed
	if strings.Count(s, "+") > 1 {
		return SemVer{}, fmt.Errorf("invalid semver: multiple +")
	}
	// Only one - allowed before + (pre-release), but - allowed in build metadata
	plusIdx := strings.Index(s, "+")
	dashIdx := strings.Index(s, "-")
	if dashIdx != -1 && (plusIdx == -1 || dashIdx < plusIdx) {
		preEnd := plusIdx
		if preEnd == -1 {
			preEnd = len(s)
		}
		pre := s[dashIdx:preEnd]
		if strings.Contains(pre, "--") {
			return SemVer{}, fmt.Errorf("invalid semver: double -- in pre-release")
		}
		if strings.Count(s[:preEnd], "-") > 1 {
			return SemVer{}, fmt.Errorf("invalid semver: multiple - in pre-release")
		}
	}
	coreEnd := len(s)
	if dashIdx != -1 && (plusIdx == -1 || dashIdx < plusIdx) {
		coreEnd = dashIdx
	}
	if plusIdx != -1 && plusIdx < coreEnd {
		coreEnd = plusIdx
	}
	core := s[:coreEnd]
	parts := strings.Split(core, ".")
	if len(parts) != 2 && len(parts) != 3 {
		return SemVer{}, fmt.Errorf("invalid version %q (want MAJOR.MINOR or MAJOR.MINOR.PATCH)", s)
	}
	toInt := func(p string) (int, error) {
		if p == "" {
			return 0, fmt.Errorf("empty version segment")
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid numeric segment %q", p)
		}
		return n, nil
	}
	maj, err := toInt(parts[0])
	if err != nil {
		return SemVer{}, err
	}
	min, err := toInt(parts[1])
	if err != nil {
		return SemVer{}, err
	}
	v := SemVer{Major: maj, Minor: min, Patch: 0}
	if len(parts) == 3 {
		patch, err := toInt(parts[2])
		if err != nil {
			return SemVer{}, err
		}
		v.Patch = patch
	}
	return v, nil
}

// Cmp compares two SemVer values: -1 if a<b, 0 if a==b, 1 if a>b.
func Cmp(a, b SemVer) int {
	if a.Major != b.Major {
		if a.Major < b.Major {
			return -1
		}
		return 1
	}
	if a.Minor != b.Minor {
		if a.Minor < b.Minor {
			return -1
		}
		return 1
	}
	if a.Patch != b.Patch {
		if a.Patch < b.Patch {
			return -1
		}
		return 1
	}
	return 0
}

// String prints MAJOR.MINOR.PATCH
func (v SemVer) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// ------------------------
// Version ranges
// ------------------------

// VersionRange is >= Min and, if Max present and !InclusiveMax, < Max
type VersionRange struct {
	Min          *SemVer
	Max          *SemVer
	InclusiveMax bool
}

// InRange reports whether v satisfies the range r.
func InRange(v SemVer, r VersionRange) bool {
	if r.Min != nil && Cmp(v, *r.Min) < 0 {
		return false
	}
	if r.Max != nil {
		cmpResult := Cmp(v, *r.Max)
		if cmpResult > 0 || (cmpResult == 0 && !r.InclusiveMax) {
			return false
		}
	}
	return true
}

// Intersection returns the intersection of the provided version ranges. If
// the intersection is empty, an empty VersionRange will be returned. If no
// ranges are provided, or all provided ranges are empty, nil will be returned.
//
// The value of InclusiveMax in the returned VersionRange will be determined
// by its value in the range that constrains the max. If there is no max constraint,
// it will be unset (and therefore default to false).
func Intersection(ranges ...VersionRange) *VersionRange {
	if len(ranges) == 0 {
		return nil
	}
	var greatestMin *SemVer
	var smallestMax *SemVer
	var inclusiveMax bool
	for _, versionRange := range ranges {
		if versionRange.Min != nil {
			if greatestMin == nil || Cmp(*versionRange.Min, *greatestMin) > 0 {
				greatestMin = versionRange.Min
			}
		}
		if versionRange.Max != nil {
			if smallestMax == nil || Cmp(*versionRange.Max, *smallestMax) < 0 {
				smallestMax = versionRange.Max
				inclusiveMax = versionRange.InclusiveMax
			}
		}
	}
	if greatestMin != nil && smallestMax != nil {
		// If intersection is empty, return empty range
		if Cmp(*greatestMin, *smallestMax) > 0 {
			return &VersionRange{}
		}
	} else if greatestMin == nil && smallestMax == nil {
		return nil
	}
	return &VersionRange{Min: greatestMin, Max: smallestMax, InclusiveMax: inclusiveMax}
}

// FindDisjointRanges checks the provided list of ranges to see if they are disjoint. If
// so, it returns the indices of two ranges which are disjoint. There may be more than 2
// such ranges; FindDisjointRanges doesn't guarantee which pair of ranges will be identified,
// but its choice is deterministic. If the entire list of ranges has an intersection, it
// returns (-1, -1).
func FindDisjointRanges(ranges []VersionRange) (int, int) {
	completeIntersection := Intersection(ranges...)
	if completeIntersection == nil || *completeIntersection != (VersionRange{}) {
		return -1, -1
	}
	for i, vr1 := range ranges {
		subsetIntersection := Intersection(ranges[i+1:]...)
		if subsetIntersection == nil || *subsetIntersection != (VersionRange{}) {
			// vr1 is mutually exclusive with some range after it; find this range
			for j, vr2 := range ranges[i:] {
				inter := Intersection(vr1, vr2)
				if inter != nil && *inter == (VersionRange{}) {
					return i, i + j
				}
			}
		}
	}
	// This case should never be hit
	return -1, -1
}

// String renders the min inclusive > max (exclusive) constraint in a readable way.
func (vr VersionRange) String() string {
	var parts []string
	if vr.Min != nil {
		parts = append(parts, fmt.Sprintf(">= %s", vr.Min.String()))
	}
	if vr.Max != nil {
		parts = append(parts, fmt.Sprintf("< %s", vr.Max.String()))
	}
	if len(parts) == 0 {
		return "(any)"
	}
	return strings.Join(parts, ", ")
}
