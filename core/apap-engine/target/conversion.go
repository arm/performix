// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"fmt"
	"reflect"
)

// EngineTargetFromJSON converts a Target into an internal engine target type
func EngineTargetFromJSON(t JSONTarget) (Target, error) {
	switch tgt := t.Value.(type) {
	case *JSONSSHTarget:
		var jumps []SSHHostConfig
		for _, j := range tgt.Jumps {
			jumps = append(jumps, SSHHostConfig(j))
		}
		return &SSHTarget{Jumps: jumps}, nil

	case *JSONLocalTarget:
		return &LocalTarget{}, nil

	case *JSONAndroidTarget:
		return &AndroidTarget{
			SerialNumber:    tgt.SerialNumber,
			DeviceIPAddress: tgt.DeviceIPAddress,
		}, nil

	default:
		targetType := "nil"
		if t.Value != nil {
			targetType = reflect.TypeOf(t.Value).String()
		}
		return nil, fmt.Errorf("unknown target type: %v", targetType)
	}
}

// JSONTargetFromEngine converts an internal target type (SSHTarget or LocalTarget) into a config.Target.
func JSONTargetFromEngine(t Target) (JSONTarget, error) {
	switch v := t.(type) {
	case *SSHTarget:
		// Map each jump host from the internal type to the config type.
		var jumps []JSONSSHHostConfig
		for _, j := range v.Jumps {
			jumps = append(jumps, JSONSSHHostConfig(j))
		}
		return JSONTarget{
			Value: &JSONSSHTarget{
				Jumps: jumps,
			},
		}, nil

	case *LocalTarget:
		return JSONTarget{
			Value: &JSONLocalTarget{},
		}, nil

	case *AndroidTarget:
		return JSONTarget{
			Value: &JSONAndroidTarget{
				SerialNumber:    v.SerialNumber,
				DeviceIPAddress: v.DeviceIPAddress,
			},
		}, nil

	default:
		return JSONTarget{}, fmt.Errorf("unknown target type %T", t)
	}
}
