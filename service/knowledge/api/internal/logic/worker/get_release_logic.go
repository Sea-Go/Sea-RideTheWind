// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetReleaseLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetReleaseLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetReleaseLogic {
	return &GetReleaseLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetReleaseLogic) GetRelease(req *types.ReleasePath) (resp *types.ReleaseEnvelope, err error) {
	result, err := l.svcCtx.Store.GetRelease(l.ctx, req.ReleaseId)
	if err != nil {
		return nil, err
	}
	return &types.ReleaseEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
