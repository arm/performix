// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"encoding/xml"
	"os"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

func DecodeXML[T any](data []byte) (*T, error) {
	var result T
	err := xml.Unmarshal(data, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func EncodeXML[T any](data *T) ([]byte, error) {
	result, err := xml.Marshal(data)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func ReadXMLFile[T any](filename string) (*T, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	return DecodeXML[T](data)
}

func WriteXMLFile[T any](filename string, data *T) error {
	result, err := EncodeXML(data)
	if err != nil {
		return err
	}

	return os.WriteFile(filename, result, perms.LocalFilePerm)
}
