// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

type RunUpdate struct {
	Operations []RunUpdateOperation
}

func (u RunUpdate) IsEmpty() bool {
	return len(u.Operations) == 0
}

type RunUpdateOperation interface {
	isRunUpdateOperation()
}

type SetHostSourceCodePaths struct {
	HostSourceCodePaths HostSourceCodePath
}

func (SetHostSourceCodePaths) isRunUpdateOperation() {}

type ClearHostSourceCodePaths struct{}

func (ClearHostSourceCodePaths) isRunUpdateOperation() {}

type SetRunGroup struct {
	Group string
}

func (SetRunGroup) isRunUpdateOperation() {}

type ClearRunGroup struct{}

func (ClearRunGroup) isRunUpdateOperation() {}

type SetRunTags struct {
	Tags []string
}

func (SetRunTags) isRunUpdateOperation() {}

type AddRunTags struct {
	Tags []string
}

func (AddRunTags) isRunUpdateOperation() {}

type RemoveRunTags struct {
	Tags []string
}

func (RemoveRunTags) isRunUpdateOperation() {}

type ClearRunTags struct{}

func (ClearRunTags) isRunUpdateOperation() {}

func (u RunUpdate) Normalize() (RunUpdate, error) {
	normalized := RunUpdate{Operations: make([]RunUpdateOperation, 0, len(u.Operations))}
	for _, operation := range u.Operations {
		switch op := operation.(type) {
		case SetHostSourceCodePaths:
			normalized.Operations = append(normalized.Operations, SetHostSourceCodePaths{
				HostSourceCodePaths: HostSourceCodePath{Paths: append([]string{}, op.HostSourceCodePaths.Paths...)},
			})
		case ClearHostSourceCodePaths:
			normalized.Operations = append(normalized.Operations, op)
		case SetRunGroup:
			group, err := normalizeRunGroup(op.Group)
			if err != nil {
				return RunUpdate{}, err
			}
			normalized.Operations = append(normalized.Operations, SetRunGroup{Group: group})
		case ClearRunGroup:
			normalized.Operations = append(normalized.Operations, op)
		case SetRunTags:
			tags, err := normalizeRunTags(op.Tags)
			if err != nil {
				return RunUpdate{}, err
			}
			normalized.Operations = append(normalized.Operations, SetRunTags{Tags: tags})
		case AddRunTags:
			tags, err := normalizeRunTags(op.Tags)
			if err != nil {
				return RunUpdate{}, err
			}
			normalized.Operations = append(normalized.Operations, AddRunTags{Tags: tags})
		case RemoveRunTags:
			tags, err := normalizeRunTags(op.Tags)
			if err != nil {
				return RunUpdate{}, err
			}
			normalized.Operations = append(normalized.Operations, RemoveRunTags{Tags: tags})
		case ClearRunTags:
			normalized.Operations = append(normalized.Operations, op)
		default:
			return RunUpdate{}, message.New(message.EngineRunInvalidUpdate)
		}
	}

	return normalized, nil
}

func (c *RunCollection) UpdateRun(ctx context.Context, entry RunID, update RunUpdate) error {
	update, err := update.Normalize()
	if err != nil {
		return err
	}

	if update.IsEmpty() {
		if !c.runExists(entry) {
			return message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": entry.Value})
		}
		return nil
	}

	unlock, err := c.LockRun(ctx, entry)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()

	// Check if run still exists (avoids TOCTOU)
	if !c.runExists(entry) {
		return message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": entry.Value})
	}

	var sourceCodePaths *HostSourceCodePath
	var categorization RunCategorization
	categorizationLoaded := false
	categorizationChanged := false

	ensureCategorization := func() (*RunCategorization, error) {
		if categorizationLoaded {
			return &categorization, nil
		}
		current, err := c.readCategorization(entry)
		if err != nil {
			return nil, err
		}
		categorization = current
		categorizationLoaded = true
		return &categorization, nil
	}

	for _, operation := range update.Operations {
		switch op := operation.(type) {
		case SetHostSourceCodePaths:
			paths := HostSourceCodePath{Paths: append([]string{}, op.HostSourceCodePaths.Paths...)}
			sourceCodePaths = &paths
		case ClearHostSourceCodePaths:
			sourceCodePaths = &HostSourceCodePath{Paths: []string{}}
		case SetRunGroup:
			categorization, err := ensureCategorization()
			if err != nil {
				return err
			}
			categorization.Group = op.Group
			categorizationChanged = true
		case ClearRunGroup:
			categorization, err := ensureCategorization()
			if err != nil {
				return err
			}
			categorization.Group = ""
			categorizationChanged = true
		case SetRunTags:
			categorization, err := ensureCategorization()
			if err != nil {
				return err
			}
			categorization.Tags = append([]string{}, op.Tags...)
			categorizationChanged = true
		case AddRunTags:
			categorization, err := ensureCategorization()
			if err != nil {
				return err
			}
			categorization.Tags = addRunTags(categorization.Tags, op.Tags)
			categorizationChanged = true
		case RemoveRunTags:
			categorization, err := ensureCategorization()
			if err != nil {
				return err
			}
			categorization.Tags = removeRunTags(categorization.Tags, op.Tags)
			categorizationChanged = true
		case ClearRunTags:
			categorization, err := ensureCategorization()
			if err != nil {
				return err
			}
			categorization.Tags = []string{}
			categorizationChanged = true
		}
	}

	if sourceCodePaths != nil {
		if err := c.updateHostSourceCodePaths(entry, sourceCodePaths); err != nil {
			return err
		}
	}

	if categorizationChanged {
		if err := c.writeCategorization(entry, categorization); err != nil {
			return err
		}
	}

	return nil
}

func (c *RunCollection) UpdateRuns(ctx context.Context, entries []RunID, update RunUpdate) []error {
	errs := make([]error, len(entries))
	for i, entry := range entries {
		if err := c.UpdateRun(ctx, entry, update); err != nil {
			errs[i] = err
		}
	}
	return errs
}

func (c *RunCollection) updateHostSourceCodePaths(entry RunID, sourceCodePaths *HostSourceCodePath) error {
	filePath := c.getSourceCodePath(entry)
	if err := WriteHostSourceCodePath(filePath, sourceCodePaths); err != nil {
		metadata := map[string]string{
			"runID": entry.Value,
			"path":  filePath,
		}
		return message.New(message.EngineRunUpdateSourceCodePaths).WithCause(err).WithMetadata(metadata)
	}
	return nil
}

func (c *RunCollection) writeCategorization(entry RunID, categorization RunCategorization) error {
	categorizationPath := c.getCategorizationPath(entry)
	if err := WriteRunCategorization(categorizationPath, &categorization); err != nil {
		metadata := map[string]string{
			"runID": entry.Value,
			"path":  categorizationPath,
		}
		return message.New(message.EngineRunUpdateCategorization).WithCause(err).WithMetadata(metadata)
	}
	return nil
}
