// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package progress

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestETAUpdate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var eta ETAEstimator
		require.Nil(t, eta.Update(0, 100))
		time.Sleep(30 * time.Second)
		require.Nil(t, eta.Update(1, 0))
		time.Sleep(10 * time.Second)
		require.Nil(t, eta.Update(1, 1<<63-1), "estimate exceeds uint64 seconds")
		require.Nil(t, eta.Update(50, 0))
		// Fractional percentages and progress below 1% use the actual work counts.
		require.Equal(t, uint64(1990), *eta.Update(50, 10000))
		require.Equal(t, uint64(590), *eta.Update(50, 3000))
		// Learning the total does not discard the earlier transfer time.
		require.Equal(t, uint64(10), *eta.Update(50, 100))
		time.Sleep(5 * time.Second)
		// Repeated counts refresh the estimate without restarting its clock.
		require.Equal(t, uint64(15), *eta.Update(50, 100))
		require.Equal(t, uint64(0), *eta.Update(100, 100))
	})
}

func TestETAVisibilityThreshold(t *testing.T) {
	for _, tc := range []struct {
		elapsed time.Duration
		want    uint64
	}{
		{time.Second, 0},
		{1499 * time.Millisecond, 0},
		{1500 * time.Millisecond, 2},
		{2500 * time.Millisecond, 3},
	} {
		t.Run(tc.elapsed.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				eta := NewStartedETAEstimator()
				// At 80% progress, remaining time is one quarter of elapsed time.
				time.Sleep(4 * tc.elapsed) // Advances the synctest clock, not real time.
				remaining := eta.Update(80, 100)
				if tc.want == 0 {
					require.Nil(t, remaining)
				} else {
					require.Equal(t, tc.want, *remaining)
				}
			})
		})
	}
}

func TestETAMinimumElapsed(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(map[bool]string{false: "first progress", true: "stage start"}[started], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				eta := &ETAEstimator{}
				if started {
					eta = NewStartedETAEstimator()
				} else {
					time.Sleep(10 * time.Second)
					require.Nil(t, eta.Update(0, 100))
					require.Nil(t, eta.Update(1, 0))
				}
				time.Sleep(1999 * time.Millisecond)
				require.Nil(t, eta.Update(10, 100), "hide even a large ETA before two seconds")
				time.Sleep(time.Millisecond)
				require.Equal(t, uint64(18), *eta.Update(10, 100))
			})
		})
	}
}

func TestETAResetsWhenProgressDecreases(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		eta := NewStartedETAEstimator()
		time.Sleep(10 * time.Second)
		require.Equal(t, uint64(10), *eta.Update(50, 100))

		require.Nil(t, eta.Update(0, 100))
		time.Sleep(1999 * time.Millisecond)
		require.Nil(t, eta.Update(10, 100))
		time.Sleep(time.Millisecond)
		require.Equal(t, uint64(18), *eta.Update(10, 100))
	})
}

func TestETANonZeroResetBaseline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		eta := NewStartedETAEstimator()
		eta.Update(80, 100)
		require.Nil(t, eta.Update(20, 100))
		time.Sleep(2 * time.Second)
		require.Nil(t, eta.Update(20, 100), "no work observed since reset")
		// One point completed since reset, with 79 points still remaining.
		require.Equal(t, uint64(158), *eta.Update(21, 100))
		time.Sleep(2 * time.Second)
		require.Equal(t, uint64(316), *eta.Update(21, 100), "a stall retains the baseline")
		require.Nil(t, eta.Update(10, 100), "another decrease replaces the baseline")
		time.Sleep(2 * time.Second)
		require.Equal(t, uint64(18), *eta.Update(19, 100))
		require.Equal(t, uint64(0), *eta.Update(100, 100))
	})
}
