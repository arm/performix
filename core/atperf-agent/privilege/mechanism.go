// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package privilege

// ProofMechanism identifies the requested privilege-elevation mechanism.
type ProofMechanism int

const (
	ProofMechanismUnknown ProofMechanism = iota
	NoPasswdUserns
	NoPasswdSudo
	SudoPassword
	SetuidHelper
	AndroidSu
)
