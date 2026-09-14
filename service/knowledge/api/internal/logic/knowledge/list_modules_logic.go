// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListModulesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListModulesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListModulesLogic {
	return &ListModulesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListModulesLogic) ListModules(req *types.ListModulesReq) (resp *types.ListModulesRespEnvelope, err error) {
	result, err := l.svcCtx.Store.ListModules(l.ctx, req.Limit, req.Cursor, true)
	if err != nil {
		return nil, err
	}
	return &types.ListModulesRespEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
