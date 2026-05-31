package grpcsvc

import (
	"context"
	"encoding/json"
	"time"

	"github.com/muse/adapters/sqlstore"
	"github.com/muse/gamekit/gkerr"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// callbackRetryBackoff delays the next attempt when an orchestrator reports a
// failure, and callbackDefaultMax bounds attempts when a task has no per-prize
// max. The dispatcher owns the steady-state policy; these only apply on the
// callback-reported-failure path.
const (
	callbackRetryBackoff = time.Minute
	callbackDefaultMax   = 5
)

// ListTasks returns outbox tasks for admin review (filter status/campaign/prize).
func (s *Server) ListTasks(ctx context.Context, req *gamev1.ListTasksRequest) (*gamev1.ListTasksResponse, error) {
	scope := scopeFromProto(req.GetScope())
	tasks, next, err := s.store.ListTasks(ctx, scope, sqlstore.TaskFilter{
		Status: string(domTaskStatus(req.GetStatus())), CampaignID: req.GetCampaignId(), PrizeID: req.GetPrizeId(),
	}, int(req.GetLimit()), req.GetCursor())
	if err != nil {
		return nil, s.fail(err)
	}
	out := make([]*gamev1.FulfillmentTask, 0, len(tasks))
	for i := range tasks {
		out = append(out, taskToProto(&tasks[i]))
	}
	return &gamev1.ListTasksResponse{Tasks: out, NextCursor: next}, nil
}

// GetTask returns one task by id within scope.
func (s *Server) GetTask(ctx context.Context, req *gamev1.GetTaskRequest) (*gamev1.GetTaskResponse, error) {
	scope := scopeFromProto(req.GetScope())
	t, err := s.store.GetTask(ctx, scope, req.GetTaskId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.GetTaskResponse{Task: taskToProto(t)}, nil
}

// RetryTask re-arms a failed or dead task for the dispatcher.
func (s *Server) RetryTask(ctx context.Context, req *gamev1.RetryTaskRequest) (*gamev1.RetryTaskResponse, error) {
	scope := scopeFromProto(req.GetScope())
	t, err := s.store.RetryTask(ctx, scope, req.GetTaskId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.RetryTaskResponse{Task: taskToProto(t)}, nil
}

// ReportResult finalizes a task from an external orchestrator callback (n8n).
// The task id is globally unique, so the result is applied by id; the BFF has
// already verified the HMAC at the edge.
func (s *Server) ReportResult(ctx context.Context, req *gamev1.ReportResultRequest) (*gamev1.ReportResultResponse, error) {
	taskID := req.GetTaskId()
	receipt := json.RawMessage(req.GetReceipt())
	switch req.GetStatus() {
	case gamev1.TaskStatus_TASK_STATUS_FULFILLED, gamev1.TaskStatus_TASK_STATUS_UNSPECIFIED:
		if err := s.store.CompleteTask(ctx, taskID, receipt); err != nil {
			return nil, s.fail(err)
		}
	case gamev1.TaskStatus_TASK_STATUS_FAILED:
		if _, err := s.store.FailTask(ctx, taskID, req.GetError(), callbackRetryBackoff, callbackDefaultMax, false); err != nil {
			return nil, s.fail(err)
		}
	default:
		return nil, s.fail(gkerr.Newf(gkerr.ReasonValidationFailed, "unknown callback status %q", req.GetStatus().String()))
	}
	t, err := s.store.FindTask(ctx, taskID)
	if err != nil {
		return nil, s.fail(err)
	}
	return &gamev1.ReportResultResponse{Task: taskToProto(t)}, nil
}
