// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package armcpus

// These public CPU IDs are absent from ARM-software/data/cpus.json. Keep them
// outside the generated map until that upstream catalog includes them.
// Source: Arm Telemetry Solution, commit df65af392a6fee2c39f174b1d1d467194bb4bd60,
// data/pmu/cpu/specifications/lumex/arm_c1_{nano,pro,premium,ultra}_r0p0_pmu.json.
// The names also match the collector's PMU names.
func init() {
	CpuidToName[0x41d8a] = "C1-Nano"
	CpuidToName[0x41d8b] = "C1-Pro"
	CpuidToName[0x41d90] = "C1-Premium"
	CpuidToName[0x41d8c] = "C1-Ultra"
}
