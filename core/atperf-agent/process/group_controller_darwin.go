// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import "fmt"

func newGroupController(_ GroupControllerConfig) (GroupController, error) {
	return nil, fmt.Errorf("GroupController is not supported on Darwin")
}
