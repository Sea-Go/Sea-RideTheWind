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

type FreezeWikiFactSetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewFreezeWikiFactSetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FreezeWikiFactSetLogic {
	return &FreezeWikiFactSetLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *FreezeWikiFactSetLogic) FreezeWikiFactSet(req *types.FreezeWikiFactSetReq) (resp *types.WikiFactSetEnvelope, err error) {
	record, err := l.svcCtx.Store.FreezeWikiFactSet(l.ctx,
		middleware.Actor(l.ctx.Value("userId")), *req)
	if err != nil {
		return nil, err
	}
	return &types.WikiFactSetEnvelope{Code: 200, Msg: "success", Data: record}, nil
}
