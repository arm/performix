// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/types/known/structpb"
)

// UnmarshalJSONToStruct parses a JSON string into a *structpb.Struct.
func UnmarshalJSONToStruct(jsonStr string) (*structpb.Struct, error) {
	var temp map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &temp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON to map: %w", err)
	}

	s, err := structpb.NewStruct(temp)
	if err != nil {
		return nil, fmt.Errorf("failed to convert map to structpb.Struct: %w", err)
	}

	return s, nil
}

// MarshalStructToJSON serializes a *structpb.Struct into a JSON []byte.
// If the input is nil, it returns an empty JSON object '{}'.
func MarshalStructToJSON(s *structpb.Struct) ([]byte, error) {
	if s == nil {
		return []byte(`{}`), nil
	}

	m := s.AsMap()
	jsonBytes, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal structpb.Struct to JSON: %w", err)
	}
	return jsonBytes, nil
}
