// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !confidential_telemetry

package telemetry

func cortexCPUModels() []string {
	return nil
}

func resolveCortex(string) (string, bool) {
	return "", false
}
