// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"encoding/json"
	"fmt"
)

type JSONTargetV1 struct {
	Host               string `json:"host"`
	Port               int32  `json:"port"`
	Username           string `json:"user"`
	PrivateKeyFilename string `json:"key"`
}

type JSONSSHHostConfig struct {
	Host               string        `json:"host"`
	Port               int32         `json:"port"`
	Username           string        `json:"username"`
	PrivateKeyFilename string        `json:"private_key_filename"`
	HostKeyPolicy      HostKeyPolicy `json:"host_key_policy"`
	AuthMethod         SSHAuthMethod `json:"authentication_method"`
}

type JSONSSHTarget struct {
	Jumps []JSONSSHHostConfig `json:"jumps"`
}

func (t *JSONSSHTarget) Type() TargetType {
	return TargetTypeSSH
}

type JSONLocalTarget struct {
}

func (t *JSONLocalTarget) Type() TargetType {
	return TargetTypeLocal
}

type JSONAndroidTarget struct {
	SerialNumber    string  `json:"serial_number"`
	DeviceIPAddress *string `json:"device_ip_address,omitempty"`
}

func (t *JSONAndroidTarget) Type() TargetType {
	return TargetTypeAndroid
}

type JSONSpecificTarget interface {
	Type() TargetType
}

// JSONTarget is the target struct written into the target config files
type JSONTarget struct {
	Value JSONSpecificTarget
}

func (t *JSONTarget) UnmarshalJSON(data []byte) error {
	var typeData struct {
		Type TargetType `json:"type"`
	}
	if err := json.Unmarshal(data, &typeData); err != nil {
		return err
	}

	var valueData struct {
		Value JSONSpecificTarget `json:"value"`
	}

	if typeData.Type == "" {
		// Compatibility for old version; this is not a good way to do this, but will get us over the bug for now
		// In future we should write a proper compatibility layer / versioned API to handle this in a future proof way
		var targetV1 JSONTargetV1
		if err := json.Unmarshal(data, &targetV1); err != nil {
			return err
		}

		valueData.Value = &JSONSSHTarget{Jumps: []JSONSSHHostConfig{
			{
				Host:               targetV1.Host,
				Port:               targetV1.Port,
				Username:           targetV1.Username,
				PrivateKeyFilename: targetV1.PrivateKeyFilename,
				HostKeyPolicy:      RejectHostKeyIfMissing,
			},
		}}
	} else {
		switch typeData.Type {
		case TargetTypeSSH:
			valueData.Value = &JSONSSHTarget{}
		case TargetTypeLocal:
			valueData.Value = &JSONLocalTarget{}
		case TargetTypeAndroid:
			valueData.Value = &JSONAndroidTarget{}
		default:
			return fmt.Errorf("unrecognized target type: '%s'", typeData.Type)
		}

		if err := json.Unmarshal(data, &valueData); err != nil {
			return err
		}
	}

	t.Value = valueData.Value

	return nil
}

func (t JSONTarget) MarshalJSON() ([]byte, error) {
	var typed struct {
		Type  TargetType         `json:"type"`
		Value JSONSpecificTarget `json:"value"`
	}

	if t.Value != nil {
		typed.Type = t.Value.Type()
	}
	typed.Value = t.Value

	return json.Marshal(typed)
}

type TargetType string

const (
	TargetTypeLocal   TargetType = "local"
	TargetTypeSSH     TargetType = "ssh"
	TargetTypeAndroid TargetType = "android"
)
