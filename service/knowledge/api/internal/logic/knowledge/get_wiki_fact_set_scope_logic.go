// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetWikiFactSetScopeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetWikiFactSetScopeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetWikiFactSetScopeLogic {
	return &GetWikiFactSetScopeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetWikiFactSetScopeLogic) GetWikiFactSetScope(req *types.WikiFactSetScopePath) (resp *types.WikiFactSetEnvelope, err error) {
	record, err := l.svcCtx.Store.GetWikiFactSetScope(l.ctx, *req)
	if err != nil {
		return nil, err
	}
	return &types.WikiFactSetEnvelope{Code: 200, Msg: "success", Data: record}, nil
}
