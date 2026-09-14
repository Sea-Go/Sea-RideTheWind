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

type ActivateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewActivateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ActivateLogic {
	return &ActivateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ActivateLogic) Activate(req *types.ActivateReq) (resp *types.ReleaseStateEnvelope, err error) {
	actor := middleware.Actor(l.ctx.Value("userId"))
	result, err := l.svcCtx.Store.Activate(l.ctx, actor, *req)
	if err != nil {
		return nil, err
	}
	return &types.ReleaseStateEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
