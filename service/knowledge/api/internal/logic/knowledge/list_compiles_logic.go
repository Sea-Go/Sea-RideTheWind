// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListCompilesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListCompilesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListCompilesLogic {
	return &ListCompilesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListCompilesLogic) ListCompiles(req *types.ModulePageReq) (resp *types.ListCompilesRespEnvelope, err error) {
	result, err := l.svcCtx.Store.ListCompiles(l.ctx, *req)
	if err != nil {
		return nil, err
	}
	return &types.ListCompilesRespEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
