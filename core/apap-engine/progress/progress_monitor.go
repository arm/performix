// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package progress

import (
	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type GRPCRecipeStageNotifier struct {
	Out apapproto.Apap_RecipeIssueCommandServer
	Run apapproto.RunId
}

func (p *GRPCRecipeStageNotifier) OnStageStart(stageInfo notifiers.StageInfo) {
	stageStartResponse := &apapproto.RecipeResponse_StageStart{StageStart: &apapproto.StageStart{StageName: stageInfo.Name}}
	sendErr := p.Out.Send(&apapproto.RecipeResponse{SubMessage: stageStartResponse, Id: &p.Run})
	if sendErr != nil {
		log.WithFields(log.Fields{
			"stage": stageInfo.Name,
			"runId": p.Run.Value,
		}).WithError(sendErr).Error("failed to send stage start proto message")
	}
}

func (p *GRPCRecipeStageNotifier) OnStageEnd(stageInfo notifiers.StageInfo, err error) {
	returnCode := apapproto.StatusCode_SUCCESS
	if err != nil {
		returnCode = apapproto.StatusCode_ERROR
	}

	stageFinishResponse := &apapproto.RecipeResponse_StageFinish{StageFinish: &apapproto.StageFinish{ReturnCode: returnCode, StageName: stageInfo.Name}}
	sendErr := p.Out.Send(&apapproto.RecipeResponse{SubMessage: stageFinishResponse, Id: &p.Run})
	if sendErr != nil {
		log.WithFields(log.Fields{
			"stage":      stageInfo.Name,
			"runId":      p.Run.Value,
			"returnCode": returnCode,
		}).WithError(sendErr).Error("failed to send stage end proto message")
	}
}

func (p *GRPCRecipeStageNotifier) OnStageProgress(stageInfo notifiers.StageInfo, stageProgress notifiers.StageProgress) {
	unit := StageProgressUnitToProto(stageProgress.Unit)
	stageProgressResponse := &apapproto.RecipeResponse_StageProgress{StageProgress: &apapproto.StageProgress{
		StageName: stageInfo.Name,
		Count:     stageProgress.Sent,
		Max:       stageProgress.Max,
		Unit:      unit,
		Message:   &stageProgress.Message,
	}}
	response := &apapproto.RecipeResponse{SubMessage: stageProgressResponse, Id: &p.Run}
	sendErr := p.Out.Send(response)
	if sendErr != nil {
		log.WithFields(log.Fields{
			"stage":   stageInfo.Name,
			"runId":   p.Run.Value,
			"sent":    stageProgress.Sent,
			"max":     stageProgress.Max,
			"unit":    unit.String(),
			"message": stageProgress.Message,
		}).WithError(sendErr).Error("failed to send stage progress proto message")
	}
}

func (p *GRPCRecipeStageNotifier) OnStageCancelled(stageInfo notifiers.StageInfo) {}

func (p *GRPCRecipeStageNotifier) OnRunCreated(runID run.RunID, rc *run.RunCollection) {
	p.Run.Value = runID.Value

	// Stream recipe starting message back to the client - client needs the run ID before anything else
	SendRecipeStartMessage(p.Out, runID)
}

func SendRecipeStartMessage(out apapproto.Apap_RecipeIssueCommandServer, runID run.RunID) {
	recipeStartResponse := &apapproto.RecipeResponse_RecipeStart{RecipeStart: &apapproto.RecipeStart{}}
	sendErr := out.Send(&apapproto.RecipeResponse{SubMessage: recipeStartResponse, Id: &apapproto.RunId{Value: runID.Value}})
	if sendErr != nil {
		log.WithField("runId", runID.Value).
			WithError(sendErr).
			Error("failed to send recipe start proto message")
	}
}

func (p *GRPCRecipeStageNotifier) SendRecipeFinishMessage(out apapproto.Apap_RecipeIssueCommandServer, returnCode apapproto.StatusCode, err *apapproto.ErrorChain) {
	recipeFinishResponse := &apapproto.RecipeResponse_RecipeFinish{RecipeFinish: &apapproto.RecipeFinish{ReturnCode: returnCode, Error: err}}
	sendErr := out.Send(&apapproto.RecipeResponse{SubMessage: recipeFinishResponse, Id: &p.Run})
	if sendErr != nil {
		log.WithFields(log.Fields{
			"runId":      p.Run.Value,
			"returnCode": returnCode,
		}).WithError(sendErr).Error("failed to send recipe finish proto message")
	}
}

func StageProgressUnitToProto(unit notifiers.StageProgressUnit) apapproto.StageProgressUnit {
	switch unit {
	case notifiers.UnitBytes:
		return apapproto.StageProgressUnit_UNIT_BYTES
	case notifiers.UnitPercent:
		return apapproto.StageProgressUnit_UNIT_PERCENT
	default:
		return apapproto.StageProgressUnit_UNIT_UNKNOWN
	}
}
