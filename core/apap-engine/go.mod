// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

module github.com/Arm-Debug/apap-cli/apap-engine

go 1.27.3

replace (
	github.com/Arm-Debug/apap-cli/api => ../api
	github.com/Arm-Debug/apap-cli/atperf-agent => ../atperf-agent
	github.com/Arm-Debug/apap-cli/atperf-compatibility => ../atperf-compatibility
	github.com/Arm-Debug/apap-cli/atperf-version => ../atperf-version
)

require (
	github.com/Arm-Debug/apap-cli/api v0.0.0-00010101000000-000000000000
	github.com/Arm-Debug/apap-cli/atperf-agent v0.0.0
	github.com/Arm-Debug/apap-cli/atperf-compatibility v0.0.0-00010101000000-000000000000
	github.com/Arm-Debug/apap-cli/atperf-version v0.0.0
	github.com/apache/arrow-go/v18 v18.8.0
	github.com/benbjohnson/clock v1.3.5
	github.com/bmatcuk/doublestar v1.3.4
	github.com/dop251/goja v0.0.0-20250309171923-bcd7cc6bf64c
	github.com/dop251/goja_nodejs v0.0.0-20250409162600-f7acab6894b0
	github.com/duckdb/duckdb-go/v2 v2.5.5
	github.com/gofrs/flock v0.13.1
	github.com/google/go-cmp v0.7.0
	github.com/google/uuid v1.6.0
	github.com/grpc-ecosystem/go-grpc-middleware v1.4.0
	github.com/klauspost/compress v1.20.3
	github.com/mitchellh/mapstructure v1.5.0
	github.com/pkg/sftp v1.13.12
	github.com/simukti/sqldb-logger v0.0.0-20230108155151-646c1a075551
	github.com/simukti/sqldb-logger/logadapter/logrusadapter v0.0.0-20230108155151-646c1a075551
	github.com/sirupsen/logrus v1.10.2
	github.com/spf13/afero v1.15.0
	github.com/spf13/afero/sftpfs v1.15.0
	github.com/spf13/cast v1.10.0
	github.com/spf13/viper v1.21.0
	github.com/stretchr/testify v1.12.1
	github.com/tailscale/hujson v0.0.0-20260727124030-b80ff77dac4f
	go.uber.org/atomic v1.12.0
	go.uber.org/goleak v1.3.0
	golang.org/x/crypto v0.58.0
	golang.org/x/sync v0.24.0
	golang.org/x/sys v0.49.0
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800
	google.golang.org/grpc v1.84.0
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/andybalholm/brotli v1.2.7 // indirect
	github.com/dlclark/regexp2 v1.11.5 // indirect
	github.com/duckdb/duckdb-go-bindings v0.3.3 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/darwin-amd64 v0.3.3 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/darwin-arm64 v0.3.3 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/linux-amd64 v0.3.3 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/linux-arm64 v0.3.3 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/windows-amd64 v0.3.3 // indirect
	github.com/flynn/noise v1.1.0 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/go-sourcemap/sourcemap v2.1.4+incompatible // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/google/pprof v0.0.0-20250208200701-d0013a598941 // indirect
	github.com/klauspost/cpuid/v2 v2.4.0 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/pierrec/lz4/v4 v4.1.29 // indirect
	github.com/sagikazarmark/locafero v0.11.0 // indirect
	github.com/sourcegraph/conc v0.3.1-0.20240121214520-5f936abd7ae8 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/stretchr/objx v0.5.3 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/exp v0.0.0-20260112195511-716be5621a96 // indirect
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)
