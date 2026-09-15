// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetWikiPageHeadLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetWikiPageHeadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetWikiPageHeadLogic {
	return &GetWikiPageHeadLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetWikiPageHeadLogic) GetWikiPageHead(req *types.WikiPageHeadPath) (resp *types.WikiPageHeadEnvelope, err error) {
	head, err := l.svcCtx.Store.GetWikiPageHead(l.ctx, *req)
	if err != nil {
		return nil, err
	}
	return &types.WikiPageHeadEnvelope{Code: 200, Msg: "success", Data: head}, nil
}
