// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetModuleReleaseLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetModuleReleaseLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetModuleReleaseLogic {
	return &GetModuleReleaseLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetModuleReleaseLogic) GetModuleRelease(req *types.ModuleReleasePath) (resp *types.ReleaseEnvelope, err error) {
	result, err := l.svcCtx.Store.GetModuleRelease(l.ctx, req.ModuleId, req.ReleaseId)
	if err != nil {
		return nil, err
	}
	return &types.ReleaseEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
