// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

module github.com/Arm-Debug/apap-cli/atperf-version

go 1.26.5

replace (
	github.com/Arm-Debug/apap-cli/apap-engine => ../apap-engine
	github.com/Arm-Debug/apap-cli/atperf-agent => ../atperf-agent
	github.com/Arm-Debug/apap-cli/atperf-compatibility => ../atperf-compatibility
	github.com/Arm-Debug/apap-cli/atperf-version => ../atperf-version
	github.com/Arm-Debug/apap-cli/clients/go => ../clients/go
)

require github.com/Arm-Debug/apap-cli/apap-engine v0.0.0-00010101000000-000000000000
