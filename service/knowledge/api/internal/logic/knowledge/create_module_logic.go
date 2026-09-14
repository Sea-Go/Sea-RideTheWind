// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"
	"sea-try-go/service/knowledge/api/internal/middleware"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateModuleLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateModuleLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateModuleLogic {
	return &CreateModuleLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateModuleLogic) CreateModule(req *types.CreateModuleReq) (resp *types.ModuleEnvelope, err error) {
	actor := middleware.Actor(l.ctx.Value("userId"))
	result, err := l.svcCtx.Store.CreateModule(l.ctx, actor, *req)
	if err != nil {
		return nil, err
	}
	return &types.ModuleEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
