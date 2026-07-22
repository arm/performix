// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func getUnsetFlags() pflag.FlagSet {
	flags := pflag.FlagSet{}
	flags.Int32("pid", -2, "")
	flags.String("workload", "", "")
	flags.Bool("system-wide", false, "")
	flags.String("android-package", "", "")
	flags.String("android-activity", "", "")
	return flags
}

func TestValidateFlags(t *testing.T) {
	var pid = MinPID
	workload := "myWorkload"

	t.Run("workload and pid mutually exclusive", func(t *testing.T) {
		flags := getUnsetFlags()
		err := flags.Parse([]string{"--workload", workload, "--pid", fmt.Sprint(pid)})
		assert.NoError(t, err)
		_, err = ValidateWorkloadFlags(&flags, pid, workload, ReadyCommandType)
		expectedMetadata := map[string]string{
			"flag1": "--pid",
			"flag2": "--workload",
			"cmd":   string(ReadyCommandType),
		}
		expectedErr := message.New(message.CliCmdValidationMutuallyExclusiveFlags).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
	t.Run("system-wide and pid mutually exclusive", func(t *testing.T) {
		flags := getUnsetFlags()
		err := flags.Parse([]string{"--system-wide", "--pid", fmt.Sprint(pid)})
		assert.NoError(t, err)
		_, err = ValidateWorkloadFlags(&flags, pid, workload, ReadyCommandType)
		expectedMetadata := map[string]string{
			"flag1": "--pid",
			"flag2": "--system-wide",
			"cmd":   string(ReadyCommandType),
		}
		expectedErr := message.New(message.CliCmdValidationMutuallyExclusiveFlags).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
	t.Run("workload and system-wide mutually exclusive", func(t *testing.T) {
		flags := getUnsetFlags()
		err := flags.Parse([]string{"--system-wide", "--workload", workload})
		assert.NoError(t, err)
		_, err = ValidateWorkloadFlags(&flags, pid, workload, ReadyCommandType)
		expectedMetadata := map[string]string{
			"flag1": "--workload",
			"flag2": "--system-wide",
			"cmd":   string(ReadyCommandType),
		}
		expectedErr := message.New(message.CliCmdValidationMutuallyExclusiveFlags).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
	t.Run("workload or pid or system-wide not required", func(t *testing.T) {
		flags := getUnsetFlags()
		err := flags.Parse([]string{})
		assert.NoError(t, err)

		_, err = ValidateWorkloadFlags(&flags, pid, workload, ReadyCommandType)
		assert.NoError(t, err)
	})
	t.Run("error if out of range pid", func(t *testing.T) {
		var badPid int32 = -1
		flags := getUnsetFlags()
		err := flags.Parse([]string{"--pid", fmt.Sprint(badPid)})
		assert.NoError(t, err)

		_, err = ValidateWorkloadFlags(&flags, badPid, workload, ReadyCommandType)
		metadata := map[string]string{
			"value":  fmt.Sprint(badPid),
			"minPid": strconv.Itoa(int(MinPID)),
			"maxPid": strconv.Itoa(int(MaxPID)),
			"cmd":    string(ReadyCommandType),
		}
		expectedErr := message.New(message.CliCmdRecipeCommonInvalidPid).WithMetadata(metadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
	t.Run("error if reserved pid", func(t *testing.T) {
		reservedPid := []int32{0}

		for _, pid := range reservedPid {
			flags := getUnsetFlags()

			err := flags.Parse([]string{"--pid", fmt.Sprint(pid)})
			assert.NoError(t, err)

			_, err = ValidateWorkloadFlags(&flags, pid, workload, ReadyCommandType)
			metadata := map[string]string{
				"value":  fmt.Sprint(pid),
				"minPid": strconv.Itoa(int(MinPID)),
				"maxPid": strconv.Itoa(int(MaxPID)),
				"cmd":    string(ReadyCommandType),
			}
			expectedErr := message.New(message.CliCmdRecipeCommonInvalidPid).WithMetadata(metadata)
			assert.Equal(t, expectedErr, err)
			assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		}
	})
	t.Run("returns a Workload with the specified pid if valid flags", func(t *testing.T) {
		flags := getUnsetFlags()
		err := flags.Parse([]string{"--pid", fmt.Sprint(pid)})
		assert.NoError(t, err)

		var workloadCtx Workload
		workloadCtx, err = ValidateWorkloadFlags(&flags, pid, workload, ReadyCommandType)
		assert.NoError(t, err)
		assert.Equal(t, pid, workloadCtx.PID)
	})
	t.Run("returns a Workload with the specified workload if valid flags", func(t *testing.T) {
		flags := getUnsetFlags()
		err := flags.Parse([]string{"--workload", workload})
		assert.NoError(t, err)

		var workloadCtx Workload
		workloadCtx, err = ValidateWorkloadFlags(&flags, pid, workload, ReadyCommandType)
		assert.NoError(t, err)
		assert.Equal(t, workload, workloadCtx.Command)
	})
	t.Run("returns a Workload with system-wide pid if valid flags", func(t *testing.T) {
		var systemWidePid int32 = -1
		flags := getUnsetFlags()
		err := flags.Parse([]string{"--system-wide"})
		assert.NoError(t, err)

		var workloadCtx Workload
		workloadCtx, err = ValidateWorkloadFlags(&flags, pid, workload, ReadyCommandType)
		assert.NoError(t, err)
		assert.Equal(t, systemWidePid, workloadCtx.PID)
	})
	t.Run("returns an Android launch workload for Android flags", func(t *testing.T) {
		flags := getUnsetFlags()
		err := flags.Parse([]string{"--android-package", "com.example.app", "--android-activity", ".MainActivity"})
		assert.NoError(t, err)

		workloadCtx, err := ValidateWorkloadFlags(&flags, pid, workload, ReadyCommandType)
		assert.NoError(t, err)
		assert.True(t, workloadCtx.AndroidLaunch)
	})
	t.Run("Android package and activity flags must be provided together", func(t *testing.T) {
		tests := []struct {
			name         string
			args         []string
			flag         string
			requiredFlag string
		}{
			{
				name:         "package without activity",
				args:         []string{"--android-package", "com.example.app"},
				flag:         "--android-package",
				requiredFlag: "--android-activity",
			},
			{
				name:         "activity without package",
				args:         []string{"--android-activity", ".MainActivity"},
				flag:         "--android-activity",
				requiredFlag: "--android-package",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				flags := getUnsetFlags()
				err := flags.Parse(tt.args)
				assert.NoError(t, err)

				_, err = ValidateWorkloadFlags(&flags, pid, workload, ReadyCommandType)
				expectedErr := message.New(message.CliCmdValidationFlagRequiresFlag).WithMetadata(map[string]string{
					"flag":         tt.flag,
					"requiredFlag": tt.requiredFlag,
				})
				assert.Equal(t, expectedErr, err)
				assert.NoError(t, message.ValidateMetadataPlaceholders(err))
			})
		}
	})
	t.Run("Android launch and generic launch are mutually exclusive", func(t *testing.T) {
		flags := getUnsetFlags()
		err := flags.Parse([]string{"--workload", workload, "--android-package", "com.example.app", "--android-activity", ".MainActivity"})
		assert.NoError(t, err)

		_, err = ValidateWorkloadFlags(&flags, pid, workload, ReadyCommandType)
		expectedErr := message.New(message.CliCmdValidationMutuallyExclusiveFlags).WithMetadata(map[string]string{
			"flag1": "--workload",
			"flag2": "--android-package/--android-activity",
			"cmd":   string(ReadyCommandType),
		})
		assert.Equal(t, expectedErr, err)
	})
}

func TestParseEnvVars(t *testing.T) {
	testCases := []struct {
		name       string
		input      []string
		wantResult map[string]string
		wantErr    message.Message
	}{
		{
			name:    "rejects empty string",
			input:   []string{""},
			wantErr: message.New(message.CliCmdValidationInvalidEnvVar).WithMetadata(map[string]string{"envVarString": ""}),
		},
		{
			name:    "rejects string not containing equals",
			input:   []string{"FOO: bar"},
			wantErr: message.New(message.CliCmdValidationInvalidEnvVar).WithMetadata(map[string]string{"envVarString": "FOO: bar"}),
		},
		{
			name:       "accepts valid definition",
			input:      []string{"FOO=bar"},
			wantResult: map[string]string{"FOO": "bar"},
		},
		{
			name:       "splits on first =",
			input:      []string{"FOO=bar=baz"},
			wantResult: map[string]string{"FOO": "bar=baz"},
		},
		{
			name:       "accepts empty value",
			input:      []string{"FOO="},
			wantResult: map[string]string{"FOO": ""},
		},
		{
			name:    "rejects empty name",
			input:   []string{"=bar"},
			wantErr: message.New(message.CliCmdValidationInvalidEnvVar).WithMetadata(map[string]string{"envVarString": "=bar"}),
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := ParseEnvVars(testCase.input)
			assert.Equal(t, testCase.wantErr, err)
			assert.NoError(t, message.ValidateMetadataPlaceholders(err))

			if testCase.wantErr == nil {
				assert.Equal(t, testCase.wantResult, result)
			}
		})
	}
}
