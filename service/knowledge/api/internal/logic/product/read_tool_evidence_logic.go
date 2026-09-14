// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package product

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ReadToolEvidenceLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewReadToolEvidenceLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReadToolEvidenceLogic {
	return &ReadToolEvidenceLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ReadToolEvidenceLogic) ReadToolEvidence(req *types.ToolReadReq) (resp *types.ToolReadEnvelope, err error) {
	subject, err := toolSubject(l.ctx, l.svcCtx)
	if err != nil {
		return nil, err
	}
	parent, err := l.svcCtx.Store.GetToolParent(l.ctx, subject, req.SessionId, req.OperationId)
	if err != nil {
		return nil, err
	}
	result, err := l.svcCtx.Store.ReadToolEvidence(l.ctx, parent, req.IdempotencyKey,
		req.SearchId, req.EvidenceId)
	if err != nil {
		return nil, err
	}
	return &types.ToolReadEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
