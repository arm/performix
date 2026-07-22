// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

type NullStageNotifier struct{}

func (n *NullStageNotifier) OnStageStart(stageInfo notifiers.StageInfo)          {}
func (n *NullStageNotifier) OnStageEnd(stageInfo notifiers.StageInfo, err error) {}
func (n *NullStageNotifier) OnStageProgress(stageInfo notifiers.StageInfo, stageProgress notifiers.StageProgress) {
}
func (n *NullStageNotifier) OnStageCancelled(stageInfo notifiers.StageInfo)      {}
func (n *NullStageNotifier) OnRunCreated(runID run.RunID, rc *run.RunCollection) {}

// CompositeStageNotifier dispatches stage notifications to multiple notifiers.
type CompositeStageNotifier struct {
	notifiers []notifiers.StageNotifier
}

// NewCompositeStageNotifier creates a new CompositeStageNotifier.
func NewCompositeStageNotifier(notifiers ...notifiers.StageNotifier) *CompositeStageNotifier {
	return &CompositeStageNotifier{notifiers: notifiers}
}

// OnStageStart calls OnStageStart on all underlying notifiers.
func (c *CompositeStageNotifier) OnStageStart(stageInfo notifiers.StageInfo) {
	for _, n := range c.notifiers {
		n.OnStageStart(stageInfo)
	}
}

// OnStageEnd calls OnStageEnd on all underlying notifiers.
func (c *CompositeStageNotifier) OnStageEnd(stageInfo notifiers.StageInfo, err error) {
	for _, n := range c.notifiers {
		n.OnStageEnd(stageInfo, err)
	}
}

// OnStageProgress calls OnStageProgress on all underlying notifiers.
func (c *CompositeStageNotifier) OnStageProgress(stageInfo notifiers.StageInfo, stageProgress notifiers.StageProgress) {
	for _, n := range c.notifiers {
		n.OnStageProgress(stageInfo, stageProgress)
	}
}

// OnStageCancelled calls OnStageCancelled on all underlying notifiers.
func (c *CompositeStageNotifier) OnStageCancelled(stageInfo notifiers.StageInfo) {
	for _, n := range c.notifiers {
		n.OnStageCancelled(stageInfo)
	}
}

// OnRunCreated calls OnRunCreated on all underlying notifiers.
func (c *CompositeStageNotifier) OnRunCreated(runID run.RunID, rc *run.RunCollection) {
	for _, n := range c.notifiers {
		n.OnRunCreated(runID, rc)
	}
}

// LoggingStageNotifier logs stage notifications using a logrus.FieldLogger.
// Progress updates are intentionally omitted to reduce noise.
type LoggingStageNotifier struct {
	log logrus.FieldLogger
}

func stageLogFields(event string, stageInfo notifiers.StageInfo) logrus.Fields {
	fields := logrus.Fields{
		"event":     event,
		"stageName": stageInfo.Name,
	}
	if stageInfo.Count > 0 {
		fields["stageNum"] = stageInfo.Num
		fields["totalStagesCount"] = stageInfo.Count
	}
	return fields
}

// NewLoggingStageNotifier creates a new LoggingStageNotifier.
func NewLoggingStageNotifier(log logrus.FieldLogger) *LoggingStageNotifier {
	return &LoggingStageNotifier{
		log: log,
	}
}

// OnStageStart logs the start of a stage.
func (l *LoggingStageNotifier) OnStageStart(stageInfo notifiers.StageInfo) {
	l.log.WithFields(stageLogFields("stage_start", stageInfo)).Info("Stage started")
}

// OnStageEnd logs the end of a stage, including any error.
func (l *LoggingStageNotifier) OnStageEnd(stageInfo notifiers.StageInfo, err error) {
	entry := l.log.WithFields(stageLogFields("stage_end", stageInfo))

	if err != nil {
		entry.WithError(err).Error("Stage failed")
	} else {
		entry.Info("Stage completed successfully")
	}
}

func (l *LoggingStageNotifier) OnStageProgress(stageInfo notifiers.StageInfo, stageProgress notifiers.StageProgress) {
	// OnStageProgress is intentionally a no-op function to avoid log spam.
}

func (l *LoggingStageNotifier) OnStageCancelled(stageInfo notifiers.StageInfo) {
	l.log.WithFields(stageLogFields("stage_cancelled", stageInfo)).Warn("Stage interrupted by cancellation")
}

// OnRunCreated logs the creation of a new run.
func (l *LoggingStageNotifier) OnRunCreated(runID run.RunID, rc *run.RunCollection) {
	l.log.WithField("run_id", runID).Infof("New run created at '%s'", rc.GetRunPath(runID))
}

// ReadinessNotifier is called after a ready stage is executed, to store the output.
type ReadinessNotifier interface {
	OnReadinessProbed(r ReadyOutput)
}

// NullReadinessNotifier implements ReadinessNotifier and is used for recipe stages that are not ready stages.
type NullReadinessNotifier struct{}

func (c *NullReadinessNotifier) OnReadinessProbed(r ReadyOutput) {}

// TargetSupportNotifier is called after a target support check is executed, to store the output.
type TargetSupportNotifier interface {
	OnTargetSupportChecked(ps deploymentsupport.PlatformSupport)
}

// NullTargetSupportNotifier implements TargetSupportNotifier and is used for recipe stages that do not check target support.
type NullTargetSupportNotifier struct{}

func (c *NullTargetSupportNotifier) OnTargetSupportChecked(ps deploymentsupport.PlatformSupport) {}

// RenderNotifier is called after a render stage is executed, to store the output.
type RenderNotifier interface {
	// OnRender performs checks on the uniqueness of renderer IDs and visualization IDs, then
	// stores the output in one single RenderOutput. Multiple calls to this function
	// (i.e from multiple render stages) will aggregate the data.
	OnRender(r RenderOutput) error
}

// NullRenderNotifier implements RenderNotifier and is used for for recipe stages that are not render stages.
type NullRenderNotifier struct{}

func (c *NullRenderNotifier) OnRender(r RenderOutput) error {
	return nil
}

type ParameterValidationError struct {
	ParameterId string
	Value       string
	Message     message.Message
}

// ParamValidation is the result of a parameter validation stage, presence of any errors indicates failed validation
type ParamValidation struct {
	Errors []ParameterValidationError
	// ValidationCompleted indicates whether params were able to validated (i.e. whether stage error is a
	// validation error, or an unexpected method error)
	ValidationCompleted bool
}

type ParameterOptions struct {
	RadioOptions        [][]parameters.ParameterOption
	SingleSelectOptions [][]parameters.ParameterOption
	MultiSelectOptions  [][]parameters.ParameterOption
}
