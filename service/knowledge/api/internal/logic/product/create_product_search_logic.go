// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package product

import (
	"context"
	"errors"
	"reflect"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/telemetry"
	"sea-try-go/service/knowledge/api/internal/types"
	"sea-try-go/service/user/user/identity"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateProductSearchLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateProductSearchLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateProductSearchLogic {
	return &CreateProductSearchLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateProductSearchLogic) CreateProductSearch(req *types.ProductSearchReq) (resp *types.ProductSearchEnvelope, err error) {
	subject, err := identity.ResolveSubjectRef(l.ctx, l.svcCtx.UserRpc)
	if err != nil {
		return nil, err
	}
	if l.svcCtx.Config.SearchSummary.Endpoint == "" {
		return &types.ProductSearchEnvelope{Code: 503, Msg: "search summary unavailable"}, nil
	}
	op, err := l.svcCtx.Store.ReserveProductSearch(l.ctx, types.AcceptedSubjectRef{
		AuthorityId: subject.AuthorityID, TenantId: subject.TenantID, SubjectId: subject.SubjectID,
	}, req.SessionId, req.IdempotencyKey, model.ProductSearchInput{
		ModuleID: req.ModuleId, Query: req.Query, Depth: req.Depth, Intelligence: req.Intelligence,
	}, l.svcCtx.Config.SearchSummary.ScopeVersion)
	if err != nil {
		return nil, err
	}
	productCtx, finish := l.svcCtx.Store.Observability.Begin(l.ctx, "knowledge.product.search", "search:"+op.SearchID,
		map[string]any{"search_id": op.SearchID, "answer_id": op.AnswerID, "release_id": op.Snapshot.ReleaseId,
			"generation": op.Snapshot.Generation, "publication_revision": op.Snapshot.PublicationRevision})
	l.ctx = productCtx
	defer func() {
		var cause error
		if err != nil {
			cause = err
		} else if resp != nil && resp.Code >= 500 {
			cause = errSearchUpstream
		}
		var info telemetry.ErrorInfo
		if cause != nil {
			info = telemetry.Unexpected(cause)
		}
		status := 0
		if resp != nil {
			status = resp.Code
		}
		finish(cause, info, map[string]any{"http_status": status, "attempt": op.Attempt})
	}()
	if accepted, verifyErr := l.svcCtx.Store.VerifiedProductSearch(l.ctx, op); verifyErr == nil {
		if err = l.svcCtx.Store.CompleteProductSearch(l.ctx, op); err != nil {
			return nil, err
		}
		return &types.ProductSearchEnvelope{Code: 200, Msg: "success", Data: accepted}, nil
	} else if !errors.Is(verifyErr, model.ErrNotFound) {
		return &types.ProductSearchEnvelope{Code: 503, Msg: "accepted answer unavailable",
			Data: productSearchPending(op, "retryable_failure")}, nil
	}
	op, claimed, err := l.svcCtx.Store.ClaimProductSearch(l.ctx, op)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return &types.ProductSearchEnvelope{Code: 202, Msg: "search in flight",
			Data: productSearchPending(op, "in_flight")}, nil
	}
	upstream, upstreamErr := callBTWSummary(l.ctx, l.svcCtx, op)
	// A timeout, cancellation, or lost HTTP reply cannot disprove a committed
	// answer. Reconcile from the authoritative store with a short bounded read.
	recovery, cancel := context.WithTimeout(context.WithoutCancel(l.ctx), 3*time.Second)
	defer cancel()
	accepted, verifyErr := l.svcCtx.Store.VerifiedProductSearch(recovery, op)
	if verifyErr == nil {
		if upstreamErr == nil && !reflect.DeepEqual(upstream, accepted) {
			_ = l.svcCtx.Store.FailProductSearch(recovery, op, "BTW_RESULT_MISMATCH")
			return &types.ProductSearchEnvelope{Code: 503, Msg: "search result mismatch",
				Data: productSearchPending(op, "retryable_failure")}, nil
		}
		if err = l.svcCtx.Store.CompleteProductSearch(recovery, op); err != nil {
			return &types.ProductSearchEnvelope{Code: 503, Msg: "search acceptance unavailable",
				Data: productSearchPending(op, "retryable_failure")}, nil
		}
		return &types.ProductSearchEnvelope{Code: 200, Msg: "success", Data: accepted}, nil
	}
	code := "ACCEPTED_ANSWER_MISSING"
	if upstreamErr != nil {
		code = "BTW_SUMMARY_UNAVAILABLE"
	}
	_ = l.svcCtx.Store.FailProductSearch(recovery, op, code)
	return &types.ProductSearchEnvelope{Code: 503, Msg: "search summary unavailable",
		Data: productSearchPending(op, "retryable_failure")}, nil
}

func productSearchPending(op model.ProductSearchOperation, status string) types.ProductSearchResult {
	return types.ProductSearchResult{SearchId: op.SearchID, AnswerId: op.AnswerID,
		Status: status, Citations: []types.ProductSearchCitation{}}
}
