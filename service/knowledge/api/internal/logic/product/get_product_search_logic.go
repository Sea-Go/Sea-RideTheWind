// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package product

import (
	"context"
	"errors"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/telemetry"
	"sea-try-go/service/knowledge/api/internal/types"
	"sea-try-go/service/user/user/identity"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetProductSearchLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetProductSearchLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetProductSearchLogic {
	return &GetProductSearchLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetProductSearchLogic) GetProductSearch(req *types.ProductSearchPath) (resp *types.ProductSearchEnvelope, err error) {
	subject, err := identity.ResolveSubjectRef(l.ctx, l.svcCtx.UserRpc)
	if err != nil {
		return nil, err
	}
	op, err := l.svcCtx.Store.GetProductSearchByID(l.ctx, types.AcceptedSubjectRef{
		AuthorityId: subject.AuthorityID, TenantId: subject.TenantID, SubjectId: subject.SubjectID,
	}, req.SessionId, req.SearchId)
	if err != nil {
		return nil, err
	}
	productCtx, finish := l.svcCtx.Store.Observability.Begin(l.ctx, "knowledge.product.search.get", "search:"+op.SearchID,
		map[string]any{"search_id": op.SearchID, "answer_id": op.AnswerID})
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
		finish(cause, info, map[string]any{"http_status": status, "operation_status": op.Status})
	}()
	accepted, verifyErr := l.svcCtx.Store.VerifiedProductSearch(l.ctx, op)
	if verifyErr == nil {
		if err = l.svcCtx.Store.CompleteProductSearch(l.ctx, op); err != nil {
			return nil, err
		}
		return &types.ProductSearchEnvelope{Code: 200, Msg: "success", Data: accepted}, nil
	}
	if !errors.Is(verifyErr, model.ErrNotFound) || op.Status == "committed" {
		return &types.ProductSearchEnvelope{Code: 503, Msg: "accepted answer unavailable",
			Data: productSearchPending(op, "retryable_failure")}, nil
	}
	if op.Status == "failed" {
		return &types.ProductSearchEnvelope{Code: 200, Msg: "retryable failure",
			Data: productSearchPending(op, "retryable_failure")}, nil
	}
	return &types.ProductSearchEnvelope{Code: 202, Msg: "search in flight",
		Data: productSearchPending(op, "in_flight")}, nil
}
