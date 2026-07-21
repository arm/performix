// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package messageutil

import "testing"

func TestIsCatalogMetadataKey(t *testing.T) {
	testCases := []struct {
		name string
		key  string
		want bool
	}{
		{
			name: "metadata root key",
			key:  "metadata",
			want: true,
		},
		{
			name: "schema metadata key",
			key:  "_schema",
			want: true,
		},
		{
			name: "message namespace key",
			key:  "engine",
			want: false,
		},
		{
			name: "prefix collision is not metadata",
			key:  "metadataExtra",
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsCatalogMetadataKey(tc.key)
			if got != tc.want {
				t.Fatalf("IsCatalogMetadataKey(%q) = %v, want %v", tc.key, got, tc.want)
			}
		})
	}
}
