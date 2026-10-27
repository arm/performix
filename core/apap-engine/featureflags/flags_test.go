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
