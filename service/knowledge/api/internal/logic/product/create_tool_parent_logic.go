// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package product

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateToolParentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateToolParentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateToolParentLogic {
	return &CreateToolParentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateToolParentLogic) CreateToolParent(req *types.ToolParentReq) (resp *types.ToolParentEnvelope, err error) {
	if l.svcCtx.Config.SearchTools.Endpoint == "" {
		return &types.ToolParentEnvelope{Code: 503, Msg: "search Tools unavailable"}, nil
	}
	subject, err := toolSubject(l.ctx, l.svcCtx)
	if err != nil {
		return nil, err
	}
	parent, err := l.svcCtx.Store.ReserveToolParent(l.ctx, subject, req.SessionId,
		req.IdempotencyKey, req.ModuleId, l.svcCtx.Config.SearchTools.ScopeVersion)
	if err != nil {
		return nil, err
	}
	if err = toolParentLive(parent); err != nil {
		return nil, err
	}
	return &types.ToolParentEnvelope{Code: 200, Msg: "success", Data: toolParentResult(parent,
		l.svcCtx.Config.SearchTools.AllowLowerIntelligence)}, nil
}
