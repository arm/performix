// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mocks

import (
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

type MockStageNotifier struct {
	mock.Mock
}

func (m *MockStageNotifier) OnStageStart(stageInfo notifiers.StageInfo) {
	m.Called(stageInfo)
}

func (m *MockStageNotifier) OnStageEnd(stageInfo notifiers.StageInfo, err error) {
	m.Called(stageInfo, err)
}

func (m *MockStageNotifier) OnStageProgress(stageInfo notifiers.StageInfo, stageProgress notifiers.StageProgress) {
	m.Called(stageInfo, stageProgress)
}

func (m *MockStageNotifier) OnStageCancelled(stageInfo notifiers.StageInfo) {
	m.Called(stageInfo)
}

func (m *MockStageNotifier) OnRunCreated(runID run.RunID, rc *run.RunCollection) {
	m.Called(runID, rc)
}
