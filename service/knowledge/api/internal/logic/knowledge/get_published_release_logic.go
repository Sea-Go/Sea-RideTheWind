// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"

	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetPublishedReleaseLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetPublishedReleaseLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetPublishedReleaseLogic {
	return &GetPublishedReleaseLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetPublishedReleaseLogic) GetPublishedRelease(req *types.ModulePath) (resp *types.ReleaseEnvelope, err error) {
	result, err := l.svcCtx.Store.Published(l.ctx, req.ModuleId)
	if err != nil {
		return nil, err
	}
	return &types.ReleaseEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
