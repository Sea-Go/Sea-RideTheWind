// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetWikiFactSetEventLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetWikiFactSetEventLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetWikiFactSetEventLogic {
	return &GetWikiFactSetEventLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetWikiFactSetEventLogic) GetWikiFactSetEvent(req *types.WikiFactSetEventPath) (resp *types.WikiFactSetEventEnvelope, err error) {
	record, err := l.svcCtx.Store.GetWikiFactSetEvent(l.ctx, req.EventId)
	if err != nil {
		return nil, err
	}
	return &types.WikiFactSetEventEnvelope{Code: 200, Msg: "success", Data: record}, nil
}
