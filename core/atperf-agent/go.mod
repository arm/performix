// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

module github.com/Arm-Debug/apap-cli/atperf-agent

go 1.27.3

replace (
	github.com/Arm-Debug/apap-cli/apap-engine => ../apap-engine
	github.com/Arm-Debug/apap-cli/api => ../api
	github.com/Arm-Debug/apap-cli/atperf-compatibility => ../atperf-compatibility
	github.com/Arm-Debug/apap-cli/atperf-version => ../atperf-version
)

require (
	github.com/Arm-Debug/apap-cli/apap-engine v0.0.0-00010101000000-000000000000
	github.com/Arm-Debug/apap-cli/api v0.0.0-00010101000000-000000000000
	github.com/Arm-Debug/apap-cli/atperf-version v0.0.0
	github.com/bmatcuk/doublestar v1.3.4
	github.com/flynn/noise v1.1.0
	github.com/gofrs/flock v0.13.1
	github.com/grpc-ecosystem/go-grpc-middleware v1.4.0
	github.com/sirupsen/logrus v1.10.2
	github.com/spf13/afero v1.15.0
	github.com/spf13/cobra v1.10.2
	github.com/stretchr/testify v1.12.1
	golang.org/x/sys v0.49.0
	google.golang.org/grpc v1.84.0
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/stretchr/objx v0.5.3 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800 // indirect
)
