// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetModuleCompileLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetModuleCompileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetModuleCompileLogic {
	return &GetModuleCompileLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetModuleCompileLogic) GetModuleCompile(req *types.ModuleCompilePath) (resp *types.CompileEnvelope, err error) {
	result, err := l.svcCtx.Store.GetModuleCompile(l.ctx, req.ModuleId, req.CompileId)
	if err != nil {
		return nil, err
	}
	return &types.CompileEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
