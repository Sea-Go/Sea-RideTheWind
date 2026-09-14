// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListReleasesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListReleasesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListReleasesLogic {
	return &ListReleasesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListReleasesLogic) ListReleases(req *types.ModulePageReq) (resp *types.ListReleasesRespEnvelope, err error) {
	result, err := l.svcCtx.Store.ListReleases(l.ctx, *req)
	if err != nil {
		return nil, err
	}
	return &types.ListReleasesRespEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
