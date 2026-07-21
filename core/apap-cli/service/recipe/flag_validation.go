// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"math"
	"strconv"
	"strings"

	"github.com/spf13/pflag"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

const MinPID int32 = 1
const MaxPID int32 = math.MaxInt32

type CommandType string

const (
	RunCommandType   CommandType = "recipe run"
	ReadyCommandType CommandType = "recipe ready"
)

// ValidateWorkloadFlags validates the workload selection flags. It checks for
// paired Android launch flags, mutual exclusion, and validity of pid (MinPID <= pid <= MaxPID).
//
// If all checks pass, a workload is returned with its selected mode, Command/PID values, and a default timeout of 0.
func ValidateWorkloadFlags(flags *pflag.FlagSet, pid int32, workload string, cmd CommandType) (Workload, error) {
	androidLaunchChanged, err := ValidateAndroidLaunchFlags(flags)
	if err != nil {
		return Workload{}, err
	}

	metadata := map[string]string{
		"cmd": string(cmd),
	}
	modes := []struct {
		changed bool
		flag    string
	}{
		{changed: flags.Changed("pid"), flag: "--pid"},
		{changed: flags.Changed("workload"), flag: "--workload"},
		{changed: flags.Changed("system-wide"), flag: "--system-wide"},
		{changed: androidLaunchChanged, flag: "--android-package/--android-activity"},
	}
	for i, first := range modes {
		if !first.changed {
			continue
		}
		for _, second := range modes[i+1:] {
			if second.changed {
				metadata["flag1"] = first.flag
				metadata["flag2"] = second.flag
				return Workload{}, message.New(message.CliCmdValidationMutuallyExclusiveFlags).WithMetadata(metadata)
			}
		}
	}

	// PID must be a positive integer if user-provided, not exceed 2^31-1, and not be a reserved value:
	//   0 is reserved by neoprof to indicate self profiling
	if flags.Changed("pid") && (pid < MinPID || pid > MaxPID) {
		metadata["value"] = strconv.Itoa(int(pid))
		metadata["minPid"] = strconv.Itoa(int(MinPID))
		metadata["maxPid"] = strconv.Itoa(int(MaxPID))
		return Workload{}, message.New(message.CliCmdRecipeCommonInvalidPid).WithMetadata(metadata)
	}

	// The service code is already written such that pid == -1 indicates system-wide
	if flags.Changed("system-wide") {
		pid = -1
	}

	return Workload{Command: workload, PID: pid, Timeout: 0, AndroidLaunch: androidLaunchChanged}, nil
}

// ValidateAndroidLaunchFlags requires the package and activity flags to be
// provided together and reports whether Android launch mode was selected.
func ValidateAndroidLaunchFlags(flags *pflag.FlagSet) (bool, error) {
	packageChanged := flags.Changed("android-package")
	activityChanged := flags.Changed("android-activity")
	if packageChanged == activityChanged {
		return packageChanged, nil
	}

	flag := "--android-package"
	requiredFlag := "--android-activity"
	if activityChanged {
		flag, requiredFlag = requiredFlag, flag
	}
	return false, message.New(message.CliCmdValidationFlagRequiresFlag).WithMetadata(map[string]string{
		"flag":         flag,
		"requiredFlag": requiredFlag,
	})
}

func ParseEnvVars(envs []string) (map[string]string, message.Message) {
	envMapping := make(map[string]string, len(envs))
	for _, str := range envs {
		envName, envVal, found := strings.Cut(str, "=")
		if !found || envName == "" {
			return nil, message.New(message.CliCmdValidationInvalidEnvVar).WithMetadata(map[string]string{"envVarString": str})
		}
		envMapping[envName] = envVal
	}
	return envMapping, nil
}
