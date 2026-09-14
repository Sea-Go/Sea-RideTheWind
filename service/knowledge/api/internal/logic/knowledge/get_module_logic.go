// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetModuleLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetModuleLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetModuleLogic {
	return &GetModuleLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetModuleLogic) GetModule(req *types.ModulePath) (resp *types.ModuleEnvelope, err error) {
	result, err := l.svcCtx.Store.GetModule(l.ctx, req.ModuleId, true)
	if err != nil {
		return nil, err
	}
	return &types.ModuleEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
