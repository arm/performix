// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"fmt"
	"os"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type RunCategorization struct {
	Group string   `json:"group"`
	Tags  []string `json:"tags"`
}

const CategorizationFilename = "categorization.json"
const categorizationComponentName = "categorization"
const categorizationComponentSchemaVersion = "1.0.0"

func CategorizationCT() cdf.ComponentType {
	return cdf.ComponentType{Name: categorizationComponentName, SchemaVersion: categorizationComponentSchemaVersion}
}

func normalizeRunGroup(group string) (string, error) {
	trimmedGroup := strings.TrimSpace(group)
	if trimmedGroup == "" {
		return "", message.New(message.EngineRunInvalidGroup).WithMetadata(map[string]string{"group": fmt.Sprintf("%q", group)})
	}
	return trimmedGroup, nil
}

func normalizeRunTags(tags []string) ([]string, error) {
	normalized := make([]string, 0, len(tags))
	seen := map[string]struct{}{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			return nil, message.New(message.EngineRunInvalidTags).WithMetadata(map[string]string{"tags": util.DisplayErrorStringSlice(tags)})
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		normalized = append(normalized, tag)
	}
	return normalized, nil
}

func addRunTags(existing []string, tags []string) []string {
	result := append([]string{}, existing...)
	seen := map[string]struct{}{}
	for _, tag := range result {
		seen[tag] = struct{}{}
	}
	for _, tag := range tags {
		if _, ok := seen[tag]; !ok {
			seen[tag] = struct{}{}
			result = append(result, tag)
		}
	}
	return result
}

func removeRunTags(existing []string, tags []string) []string {
	remove := map[string]struct{}{}
	for _, tag := range tags {
		remove[tag] = struct{}{}
	}
	result := make([]string, 0, len(existing))
	for _, tag := range existing {
		if _, ok := remove[tag]; ok {
			continue
		}
		result = append(result, tag)
	}
	return result
}

func AddRunCategorizationComponent(builder componentAdder) string {
	return builder.AddComponent(CategorizationCT(), CategorizationFilename)
}

func WriteRunCategorization(path string, categorization *RunCategorization) error {
	if categorization == nil {
		categorization = &RunCategorization{}
	}
	if categorization.Tags == nil {
		categorization.Tags = []string{}
	}

	err := util.WriteJSONFile(path, categorization, perms.LocalFilePerm)
	if err != nil {
		return fmt.Errorf("failed to write run categorization at %q: %w", path, err)
	}
	return nil
}

func ReadRunCategorization(path string) (*RunCategorization, error) {
	categorization, err := util.ReadJSONFile[RunCategorization](path)
	if err != nil {
		if os.IsNotExist(err) {
			return &RunCategorization{Tags: []string{}}, nil
		}
		return &RunCategorization{}, fmt.Errorf("failed to read run categorization at %q: %w", path, err)
	}
	if categorization.Tags == nil {
		categorization.Tags = []string{}
	}
	return categorization, nil
}
