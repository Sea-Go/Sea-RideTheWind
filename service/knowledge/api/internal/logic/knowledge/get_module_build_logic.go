// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetModuleBuildLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetModuleBuildLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetModuleBuildLogic {
	return &GetModuleBuildLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetModuleBuildLogic) GetModuleBuild(req *types.ModuleBuildPath) (resp *types.BuildEnvelope, err error) {
	result, err := l.svcCtx.Store.GetModuleBuild(l.ctx, req.ModuleId, req.BuildId)
	if err != nil {
		return nil, err
	}
	return &types.BuildEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
