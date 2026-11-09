// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeViewportRequestPreservesInt64AndNormalizesTracks(t *testing.T) {
	request, err := decodeViewportRequest(`{"dataset":"trace","start":-9223372036854775808,"finish":9223372036854775807,"resolution":9007199254740993,"tracks":[9223372036854775807,2,-9223372036854775808,2]}`)
	require.NoError(t, err)
	require.Equal(t, viewportRequest{
		Dataset: "trace", Start: math.MinInt64, End: math.MaxInt64,
		Resolution: 9007199254740993,
		Tracks:     []int64{math.MinInt64, 2, math.MaxInt64},
	}, request)
}

func TestDecodeViewportRequestEmptyTracks(t *testing.T) {
	request, err := decodeViewportRequest(`{"dataset":"trace","start":0,"finish":10,"resolution":1,"tracks":[]}`)
	require.NoError(t, err)
	require.Empty(t, request.Tracks)
}

func TestDecodeViewportRequestInvalid(t *testing.T) {
	for _, tc := range []struct{ name, encoded string }{
		{"malformed", `{"dataset":`},
		{"null request", `null`},
		{"missing field", `{"dataset":"trace","start":0,"finish":10,"tracks":[1]}`},
		{"null number", `{"dataset":"trace","start":null,"finish":10,"resolution":1,"tracks":[1]}`},
		{"null tracks", `{"dataset":"trace","start":0,"finish":10,"resolution":1,"tracks":null}`},
		{"null track", `{"dataset":"trace","start":0,"finish":10,"resolution":1,"tracks":[null]}`},
		{"empty interval", `{"dataset":"trace","start":10,"finish":10,"resolution":1,"tracks":[1]}`},
		{"negative resolution", `{"dataset":"trace","start":0,"finish":10,"resolution":-1,"tracks":[1]}`},
		{"overflow", `{"dataset":"trace","start":0,"finish":9223372036854775808,"resolution":1,"tracks":[1]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeViewportRequest(tc.encoded)
			require.Error(t, err)
		})
	}
}
