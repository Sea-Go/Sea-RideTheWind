// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package product

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetToolSearchLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetToolSearchLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetToolSearchLogic {
	return &GetToolSearchLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetToolSearchLogic) GetToolSearch(req *types.ToolSearchPath) (resp *types.ToolSearchEnvelope, err error) {
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
	child, err := l.svcCtx.Store.GetToolSearch(l.ctx, parent, req.SearchId)
	if err != nil {
		return nil, err
	}
	if child.Status == "complete" {
		return verifiedToolSearchEnvelope(l.ctx, l.svcCtx, parent, child)
	}
	return toolSearchEnvelope(child)
}
