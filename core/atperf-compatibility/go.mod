// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

module github.com/Arm-Debug/apap-cli/atperf-compatibility

go 1.26.5

replace (
	github.com/Arm-Debug/apap-cli/apap-engine => ../apap-engine
	github.com/Arm-Debug/apap-cli/atperf-agent => ../atperf-agent
	github.com/Arm-Debug/apap-cli/atperf-version => ../atperf-version
	github.com/Arm-Debug/apap-cli/clients/go => ../clients/go
)

require (
	github.com/Arm-Debug/apap-cli/apap-engine v0.0.0-00010101000000-000000000000
	github.com/Arm-Debug/apap-cli/atperf-version v0.0.0
	github.com/stretchr/testify v1.11.1
)

require (
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/niemeyer/pretty v0.0.0-20200227124842-a10e7caefd8e // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	gopkg.in/check.v1 v1.0.0-20200227125254-8fa46927fb4f // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
