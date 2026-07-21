// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

module github.com/Arm-Debug/apap-cli/atperf-agent

go 1.26.5

replace (
	github.com/Arm-Debug/apap-cli/apap-engine => ../apap-engine
	github.com/Arm-Debug/apap-cli/atperf-compatibility => ../atperf-compatibility
	github.com/Arm-Debug/apap-cli/atperf-version => ../atperf-version
	github.com/Arm-Debug/apap-cli/clients/go => ../clients/go
)

require (
	github.com/Arm-Debug/apap-cli/apap-engine v0.0.0-00010101000000-000000000000
	github.com/Arm-Debug/apap-cli/atperf-version v0.0.0
	github.com/Arm-Debug/apap-cli/clients/go v0.0.0-00010101000000-000000000000
	github.com/bmatcuk/doublestar v1.3.4
	github.com/flynn/noise v1.1.0
	github.com/gofrs/flock v0.13.0
	github.com/grpc-ecosystem/go-grpc-middleware v1.4.0
	github.com/sirupsen/logrus v1.9.3
	github.com/spf13/afero v1.14.0
	github.com/spf13/cobra v1.9.1
	github.com/stretchr/testify v1.11.1
	golang.org/x/sys v0.46.0
	google.golang.org/grpc v1.79.3
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/ARM-software/golang-utils/utils v1.82.1 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/spf13/cast v1.7.1 // indirect
	github.com/spf13/pflag v1.0.6 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	golang.org/x/crypto v0.53.0 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/text v0.39.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
