// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"
	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/middleware"

	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type CancelCompileLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCancelCompileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CancelCompileLogic {
	return &CancelCompileLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CancelCompileLogic) CancelCompile(req *types.CancelCompileReq) (resp *types.CompileEnvelope, err error) {
	actor := middleware.Actor(l.ctx.Value("userId"))
	result, err := l.svcCtx.Store.CancelCompile(l.ctx, actor, *req)
	if err != nil {
		return nil, err
	}
	return &types.CompileEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
