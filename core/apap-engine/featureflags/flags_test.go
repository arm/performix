// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package featureflags

import "testing"

func TestResolveFullCaptureDependency(t *testing.T) {
	for _, capture := range []bool{false, true} {
		for _, rerendering := range []bool{false, true} {
			configured := Flags{
				FullCaptureSupport:      capture,
				Rerendering:             rerendering,
				RunComponentCompression: true,
				JfrCapture:              true,
			}
			effective := Resolve(configured)
			expected := configured
			expected.FullCaptureSupport = capture || rerendering
			if effective != expected {
				t.Errorf("Resolve(%+v) = %+v; want %+v", configured, effective, expected)
			}
			if configured.FullCaptureSupport != capture {
				t.Fatal("resolution changed configured flags")
			}
			if Resolve(effective) != effective {
				t.Fatal("resolving an effective value changed it")
			}
		}
	}
}

func TestResolveCustomPMUCaptureDependency(t *testing.T) {
	for _, tc := range []struct {
		name             string
		configured       Flags
		wantTimelineFlag bool
	}{
		{name: "both disabled", configured: Flags{}},
		{name: "timeline enabled", configured: Flags{NeoprofTimeline: true}, wantTimelineFlag: true},
		{name: "custom PMU capture enabled", configured: Flags{CustomPMUCapture: true}, wantTimelineFlag: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			effective := Resolve(tc.configured)
			if effective.NeoprofTimeline != tc.wantTimelineFlag {
				t.Errorf("Resolve(%+v).NeoprofTimeline = %t; want %t", tc.configured, effective.NeoprofTimeline, tc.wantTimelineFlag)
			}
		})
	}
}
