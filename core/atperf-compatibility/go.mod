// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

module github.com/Arm-Debug/apap-cli/atperf-compatibility

go 1.27.3

replace (
	github.com/Arm-Debug/apap-cli/apap-engine => ../apap-engine
	github.com/Arm-Debug/apap-cli/api => ../api
	github.com/Arm-Debug/apap-cli/atperf-agent => ../atperf-agent
	github.com/Arm-Debug/apap-cli/atperf-version => ../atperf-version
)

require (
	github.com/Arm-Debug/apap-cli/apap-engine v0.0.0-00010101000000-000000000000
	github.com/Arm-Debug/apap-cli/atperf-version v0.0.0
	github.com/stretchr/testify v1.12.1
)

require go.yaml.in/yaml/v3 v3.0.5 // indirect
