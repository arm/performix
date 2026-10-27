// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package capabilities

import "fmt"

// CapabilityRequest is the generic recipe-to-integration selection contract.
// Capability descriptions come from discovery and are deliberately not copied
// into the request.
type CapabilityRequest struct {
	Required []string `json:"required"`
	Optional []string `json:"optional"`
}

// ParseCapabilityRequest copies and normalizes a present request. Absence is handled by the caller.
func ParseCapabilityRequest(value any) (*CapabilityRequest, error) {
	if _, err := object(value, []string{"required", "optional"}); err != nil {
		return nil, err
	}
	var request CapabilityRequest
	if err := decode(value, &request); err != nil {
		return nil, err
	}
	if request.Required == nil || request.Optional == nil {
		return nil, fmt.Errorf("request requires required and optional")
	}
	seen := map[string]bool{}
	normalize := func(ids []string) ([]string, error) {
		result := []string{}
		local := map[string]bool{}
		for _, id := range ids {
			if local[id] {
				continue
			}
			local[id] = true
			if seen[id] {
				return nil, fmt.Errorf("capability %q is both required and optional", id)
			}
			seen[id] = true
			if !validID.MatchString(id) {
				return nil, fmt.Errorf("invalid capability ID %q", id)
			}
			result = append(result, id)
		}
		return result, nil
	}
	var err error
	if request.Required, err = normalize(request.Required); err != nil {
		return nil, err
	}
	if request.Optional, err = normalize(request.Optional); err != nil {
		return nil, err
	}
	return &request, nil
}
