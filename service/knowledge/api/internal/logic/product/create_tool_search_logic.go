// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package product

import (
	"context"
	"encoding/json"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/telemetry"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateToolSearchLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateToolSearchLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateToolSearchLogic {
	return &CreateToolSearchLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateToolSearchLogic) CreateToolSearch(req *types.ToolSearchReq) (resp *types.ToolSearchEnvelope, err error) {
	if l.svcCtx.Config.SearchTools.Endpoint == "" {
		return &types.ToolSearchEnvelope{Code: 503, Msg: "search Tools unavailable"}, nil
	}
	subject, err := toolSubject(l.ctx, l.svcCtx)
	if err != nil {
		return nil, err
	}
	parent, err := l.svcCtx.Store.GetToolParent(l.ctx, subject, req.SessionId, req.OperationId)
	if err != nil {
		return nil, err
	}
	if err = toolParentLive(parent); err != nil {
		return nil, err
	}
	child, err := l.svcCtx.Store.ReserveToolSearch(l.ctx, parent, req.IdempotencyKey,
		model.ToolSearchInput{Query: req.Query, Depth: req.Depth, Intelligence: req.Intelligence,
			ContinueID: req.ContinueSearchId, ReadCalls: req.ReadCalls, QuoteRunes: req.QuoteRunes})
	if err != nil {
		return nil, err
	}
	productCtx, finish := l.svcCtx.Store.Observability.Begin(l.ctx, "knowledge.product.tools.search",
		"tool-search:"+child.SearchID, map[string]any{"operation_id": parent.OperationID,
			"search_id": child.SearchID, "snapshot_ref": parent.SnapshotRef,
			"budget_ref": parent.BudgetRef})
	l.ctx = productCtx
	defer func() {
		var cause error
		if err != nil {
			cause = err
		} else if resp != nil && resp.Code >= 500 {
			cause = errToolsUpstream
		}
		var info telemetry.ErrorInfo
		if cause != nil {
			info = telemetry.Unexpected(cause)
		}
		status := 0
		if resp != nil {
			status = resp.Code
		}
		finish(cause, info, map[string]any{"http_status": status, "attempt": child.Attempt})
	}()
	if child.Status == "complete" {
		return verifiedToolSearchEnvelope(l.ctx, l.svcCtx, parent, child)
	}
	child, claimed, err := l.svcCtx.Store.ClaimToolSearch(l.ctx, child)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return toolSearchEnvelope(child)
	}
	result, upstreamErr := callBTWToolsSearch(l.ctx, l.svcCtx, parent, child)
	// The lease is dispatch ownership, not a permission to project a quote.
	// A timeout after BTW citation acceptance can safely retry the same ID.
	if upstreamErr == nil {
		upstreamErr = l.svcCtx.Store.VerifyToolSearchResult(l.ctx, parent, child, result,
			l.svcCtx.Config.SearchTools.AllowLowerIntelligence)
	}
	if upstreamErr != nil {
		recovery, cancel := context.WithTimeout(context.WithoutCancel(l.ctx), 3*time.Second)
		defer cancel()
		_ = l.svcCtx.Store.FailToolSearch(recovery, child, "TOOLS_SEARCH_UNAVAILABLE")
		return &types.ToolSearchEnvelope{Code: 503, Msg: "search Tools unavailable",
			Data: types.ToolSearchResult{SearchId: child.SearchID, Status: "retryable_failure",
				Evidence: []types.ToolEvidence{}, Gaps: []string{}, Conflicts: []string{}}}, nil
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	recovery, cancel := context.WithTimeout(context.WithoutCancel(l.ctx), 3*time.Second)
	defer cancel()
	if err = l.svcCtx.Store.CompleteToolSearch(recovery, parent, child, raw,
		result.Usage.ReadCalls, result.Usage.QuoteRunes,
		l.svcCtx.Config.SearchTools.AllowLowerIntelligence); err != nil {
		_ = l.svcCtx.Store.FailToolSearch(recovery, child, "TOOLS_RESULT_UNAVAILABLE")
		return &types.ToolSearchEnvelope{Code: 503, Msg: "Tool result commit unavailable"}, nil
	}
	return &types.ToolSearchEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
