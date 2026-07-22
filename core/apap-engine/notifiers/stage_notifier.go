// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package notifiers

import (
	"time"

	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

// StageInfo carries minimal stage metadata for notifications.
type StageInfo struct {
	Name  string
	Num   int
	Count int
}

// StageProgress carries progress-specific data for a stage.
type StageProgress struct {
	Sent    int64
	Max     int64
	Unit    StageProgressUnit
	Message string
}

type StageProgressUnit int

const (
	UnitUnknown StageProgressUnit = iota
	UnitBytes
	UnitPercent
)

// StageNotifier defines callbacks at various stages of recipe execution to notify observers
type StageNotifier interface {
	OnStageStart(stageInfo StageInfo)
	OnStageEnd(stageInfo StageInfo, err error)
	OnStageProgress(stageInfo StageInfo, stageProgress StageProgress)
	OnStageCancelled(stageInfo StageInfo)
	OnRunCreated(runID run.RunID, rc *run.RunCollection)
}

const ClientFileStreamUpdateInterval = time.Millisecond * 250
const LogFileStreamUpdateInterval = time.Second * 5
