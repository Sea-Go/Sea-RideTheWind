// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetDraftModuleLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetDraftModuleLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetDraftModuleLogic {
	return &GetDraftModuleLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetDraftModuleLogic) GetDraftModule(req *types.ModulePath) (resp *types.ModuleEnvelope, err error) {
	result, err := l.svcCtx.Store.GetModule(l.ctx, req.ModuleId, false)
	if err != nil {
		return nil, err
	}
	return &types.ModuleEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
