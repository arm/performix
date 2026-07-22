// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipemigration

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

//go:embed migrations.json
var rawMigrations []byte
var migrations []migration

type migration struct {
	from    string
	to      string
	version semver.SemVer
}

func init() {
	type tempMigration struct {
		From    string `json:"from"`
		To      string `json:"to"`
		Version string `json:"version"`
	}
	var tempMigrations []tempMigration
	if err := json.Unmarshal(rawMigrations, &tempMigrations); err != nil {
		panic(fmt.Errorf("failed to load recipe name migration list: %w", err))
	}
	migrations = util.Map(tempMigrations, func(m tempMigration) migration {
		ver, err := semver.ParseSemVer(m.Version)
		if err != nil {
			panic(fmt.Errorf("migration from %v to %v is invalid; %v cannot be parsed as a semver: %w", m.From, m.To, m.Version, err))
		}
		return migration{
			from:    m.From,
			to:      m.To,
			version: ver,
		}
	})
	sortMigrations()
}

func sortMigrations() {
	slices.SortFunc(migrations, func(a, b migration) int {
		return semver.Cmp(a.version, b.version)
	})
}

// GetMigratedName attempts to migrate the provided recipe name to its current counterpart, according to the
// list of migrations defined in migrations.json. If runVer cannot be parsed, or no migrations are applied,
// the provided recipe name will be returned without change.
func GetMigratedName(runID string, recipeName string, runVer string) string {
	if runVer == "" {
		log.Warnf("GetMigratedName: run ID `%v` version is empty, skipping migration attempt", runID)
		return recipeName
	}
	runSemVer, err := semver.ParseSemVer(runVer)
	if err != nil {
		log.Warnf("GetMigratedName: run ID `%v` version `%v` cannot be parsed as a semver, skipping migration attempt", runID, runVer)
		return recipeName
	}

	migratedName := recipeName
	for _, mi := range migrations {
		if semver.Cmp(runSemVer, mi.version) >= 0 {
			continue
		}
		if mi.from == migratedName {
			migratedName = mi.to
		}
	}

	return migratedName
}
