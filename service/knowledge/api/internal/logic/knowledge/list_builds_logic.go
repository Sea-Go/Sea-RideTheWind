// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListBuildsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListBuildsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListBuildsLogic {
	return &ListBuildsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListBuildsLogic) ListBuilds(req *types.ModulePageReq) (resp *types.ListBuildsRespEnvelope, err error) {
	result, err := l.svcCtx.Store.ListBuilds(l.ctx, *req)
	if err != nil {
		return nil, err
	}
	return &types.ListBuildsRespEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
