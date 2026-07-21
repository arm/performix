// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

func main() {
	fmt.Print(versions.GetVersion())
}
