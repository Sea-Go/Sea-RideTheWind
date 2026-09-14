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

type CancelBuildLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCancelBuildLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CancelBuildLogic {
	return &CancelBuildLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CancelBuildLogic) CancelBuild(req *types.CancelBuildReq) (resp *types.BuildEnvelope, err error) {
	actor := middleware.Actor(l.ctx.Value("userId"))
	result, err := l.svcCtx.Store.CancelBuild(l.ctx, actor, *req)
	if err != nil {
		return nil, err
	}
	return &types.BuildEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
