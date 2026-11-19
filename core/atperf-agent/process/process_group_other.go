// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin

package process

// Linux uses its group controller and Windows uses its process job.
func finishProcessGroup(int, ProcessManager) error { return nil }
