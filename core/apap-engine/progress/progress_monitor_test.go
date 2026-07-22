// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package progress

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"

	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

// mockRecipeStream implements apapproto.Apap_RecipeIssueCommandServer for testing.
type mockRecipeStream struct {
	mock.Mock
	grpc.ServerStream
}

func (m *mockRecipeStream) Send(resp *apapproto.RecipeResponse) error {
	args := m.Called(resp)
	return args.Error(0)
}

func newNotifier(stream *mockRecipeStream) *GRPCRecipeStageNotifier {
	return &GRPCRecipeStageNotifier{
		Out: stream,
		Run: apapproto.RunId{Value: "test-run-id"},
	}
}

func stageInfo() notifiers.StageInfo {
	return notifiers.StageInfo{Name: "deploy", Num: 1, Count: 3}
}

// --- OnStageStart ---

func TestOnStageStart_SendsStageStartMessage(t *testing.T) {
	stream := new(mockRecipeStream)
	notifier := newNotifier(stream)
	info := stageInfo()

	stream.On("Send", mock.MatchedBy(func(resp *apapproto.RecipeResponse) bool {
		ss, ok := resp.SubMessage.(*apapproto.RecipeResponse_StageStart)
		return ok && ss.StageStart.StageName == info.Name && resp.Id.Value == "test-run-id"
	})).Return(nil).Once()

	notifier.OnStageStart(info)

	stream.AssertExpectations(t)
}

func TestOnStageStart_LogsOnSendError(t *testing.T) {
	stream := new(mockRecipeStream)
	notifier := newNotifier(stream)

	stream.On("Send", mock.Anything).Return(errors.New("send failed")).Once()

	// Should not panic; error is logged internally
	notifier.OnStageStart(stageInfo())

	stream.AssertExpectations(t)
}

// --- OnStageEnd ---

func TestOnStageEnd_SendsSuccessWhenNoError(t *testing.T) {
	stream := new(mockRecipeStream)
	notifier := newNotifier(stream)
	info := stageInfo()

	stream.On("Send", mock.MatchedBy(func(resp *apapproto.RecipeResponse) bool {
		sf, ok := resp.SubMessage.(*apapproto.RecipeResponse_StageFinish)
		return ok &&
			sf.StageFinish.StageName == info.Name &&
			sf.StageFinish.ReturnCode == apapproto.StatusCode_SUCCESS &&
			resp.Id.Value == "test-run-id"
	})).Return(nil).Once()

	notifier.OnStageEnd(info, nil)

	stream.AssertExpectations(t)
}

func TestOnStageEnd_SendsErrorWhenErrorProvided(t *testing.T) {
	stream := new(mockRecipeStream)
	notifier := newNotifier(stream)
	info := stageInfo()

	stream.On("Send", mock.MatchedBy(func(resp *apapproto.RecipeResponse) bool {
		sf, ok := resp.SubMessage.(*apapproto.RecipeResponse_StageFinish)
		return ok &&
			sf.StageFinish.StageName == info.Name &&
			sf.StageFinish.ReturnCode == apapproto.StatusCode_ERROR
	})).Return(nil).Once()

	notifier.OnStageEnd(info, errors.New("stage failed"))

	stream.AssertExpectations(t)
}

func TestOnStageEnd_LogsOnSendError(t *testing.T) {
	stream := new(mockRecipeStream)
	notifier := newNotifier(stream)

	stream.On("Send", mock.Anything).Return(errors.New("send failed")).Once()

	notifier.OnStageEnd(stageInfo(), nil)

	stream.AssertExpectations(t)
}

// --- OnStageProgress ---

func TestOnStageProgress_SendsProgressMessage(t *testing.T) {
	stream := new(mockRecipeStream)
	notifier := newNotifier(stream)
	info := stageInfo()
	progress := notifiers.StageProgress{Sent: 512, Max: 1024, Unit: notifiers.UnitBytes, Message: "Progress message"}

	stream.On("Send", mock.Anything).Return(nil).Once()

	notifier.OnStageProgress(info, progress)

	stream.AssertExpectations(t)
	sent := stream.Calls[0].Arguments[0].(*apapproto.RecipeResponse)
	sp := sent.SubMessage.(*apapproto.RecipeResponse_StageProgress).StageProgress
	assert.Equal(t, info.Name, sp.StageName)
	assert.Equal(t, int64(512), sp.Count)
	assert.Equal(t, int64(1024), sp.Max)
	assert.Equal(t, apapproto.StageProgressUnit_UNIT_BYTES, sp.Unit)
	assert.Equal(t, "Progress message", *sp.Message)
	assert.Equal(t, "test-run-id", sent.Id.Value)
}

func TestOnStageProgress_LogsOnSendError(t *testing.T) {
	stream := new(mockRecipeStream)
	notifier := newNotifier(stream)

	stream.On("Send", mock.Anything).Return(errors.New("send failed")).Once()

	notifier.OnStageProgress(stageInfo(), notifiers.StageProgress{Sent: 0, Max: 100, Unit: notifiers.UnitBytes})

	stream.AssertExpectations(t)
}

// --- OnStageCancelled ---

func TestOnStageCancelled_DoesNotPanic(t *testing.T) {
	notifier := newNotifier(new(mockRecipeStream))
	assert.NotPanics(t, func() {
		notifier.OnStageCancelled(stageInfo())
	})
}

// --- OnRunCreated ---

func TestOnRunCreated_SetsRunIDAndSendsRecipeStart(t *testing.T) {
	stream := new(mockRecipeStream)
	notifier := newNotifier(stream)
	runID := run.RunID{Value: "new-run-id"}

	stream.On("Send", mock.MatchedBy(func(resp *apapproto.RecipeResponse) bool {
		_, ok := resp.SubMessage.(*apapproto.RecipeResponse_RecipeStart)
		return ok && resp.Id.Value == "new-run-id"
	})).Return(nil).Once()

	notifier.OnRunCreated(runID, nil)

	assert.Equal(t, "new-run-id", notifier.Run.Value)
	stream.AssertExpectations(t)
}

func TestOnRunCreated_LogsOnSendError(t *testing.T) {
	stream := new(mockRecipeStream)
	notifier := newNotifier(stream)

	stream.On("Send", mock.Anything).Return(errors.New("send failed")).Once()

	notifier.OnRunCreated(run.RunID{Value: "fail-run"}, nil)

	assert.Equal(t, "fail-run", notifier.Run.Value)
	stream.AssertExpectations(t)
}

// --- SendRecipeStartMessage (standalone function) ---

func TestSendRecipeStartMessage_SendsCorrectProto(t *testing.T) {
	stream := new(mockRecipeStream)
	runID := run.RunID{Value: "start-run-id"}

	stream.On("Send", mock.MatchedBy(func(resp *apapproto.RecipeResponse) bool {
		_, ok := resp.SubMessage.(*apapproto.RecipeResponse_RecipeStart)
		return ok && resp.Id.Value == "start-run-id"
	})).Return(nil).Once()

	SendRecipeStartMessage(stream, runID)

	stream.AssertExpectations(t)
}

func TestSendRecipeStartMessage_LogsOnSendError(t *testing.T) {
	stream := new(mockRecipeStream)

	stream.On("Send", mock.Anything).Return(errors.New("send failed")).Once()

	SendRecipeStartMessage(stream, run.RunID{Value: "err-run"})

	stream.AssertExpectations(t)
}

// --- SendRecipeFinishMessage ---

func TestSendRecipeFinishMessage_SendsSuccessFinish(t *testing.T) {
	stream := new(mockRecipeStream)
	notifier := newNotifier(stream)

	stream.On("Send", mock.MatchedBy(func(resp *apapproto.RecipeResponse) bool {
		rf, ok := resp.SubMessage.(*apapproto.RecipeResponse_RecipeFinish)
		return ok &&
			rf.RecipeFinish.ReturnCode == apapproto.StatusCode_SUCCESS &&
			rf.RecipeFinish.Error == nil &&
			resp.Id.Value == "test-run-id"
	})).Return(nil).Once()

	notifier.SendRecipeFinishMessage(stream, apapproto.StatusCode_SUCCESS, nil)

	stream.AssertExpectations(t)
}

func TestSendRecipeFinishMessage_SendsErrorFinishWithErrorChain(t *testing.T) {
	stream := new(mockRecipeStream)
	notifier := newNotifier(stream)
	errChain := &apapproto.ErrorChain{Root: &apapproto.ErrorNode{Error: "something went wrong"}}

	stream.On("Send", mock.MatchedBy(func(resp *apapproto.RecipeResponse) bool {
		rf, ok := resp.SubMessage.(*apapproto.RecipeResponse_RecipeFinish)
		return ok &&
			rf.RecipeFinish.ReturnCode == apapproto.StatusCode_ERROR &&
			rf.RecipeFinish.Error.Root.Error == "something went wrong"
	})).Return(nil).Once()

	notifier.SendRecipeFinishMessage(stream, apapproto.StatusCode_ERROR, errChain)

	stream.AssertExpectations(t)
}

func TestSendRecipeFinishMessage_LogsOnSendError(t *testing.T) {
	stream := new(mockRecipeStream)
	notifier := newNotifier(stream)

	stream.On("Send", mock.Anything).Return(errors.New("send failed")).Once()

	notifier.SendRecipeFinishMessage(stream, apapproto.StatusCode_SUCCESS, nil)

	stream.AssertExpectations(t)
}
