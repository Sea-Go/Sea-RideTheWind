// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package product

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetToolParentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetToolParentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetToolParentLogic {
	return &GetToolParentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetToolParentLogic) GetToolParent(req *types.ToolParentPath) (resp *types.ToolParentEnvelope, err error) {
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
	return &types.ToolParentEnvelope{Code: 200, Msg: "success", Data: toolParentResult(parent,
		l.svcCtx.Config.SearchTools.AllowLowerIntelligence)}, nil
}
