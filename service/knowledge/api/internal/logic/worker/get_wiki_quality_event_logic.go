// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetWikiQualityEventLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetWikiQualityEventLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetWikiQualityEventLogic {
	return &GetWikiQualityEventLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetWikiQualityEventLogic) GetWikiQualityEvent(req *types.WikiQualityEventPath) (resp *types.WikiQualityEventEnvelope, err error) {
	receipt, err := l.svcCtx.Store.GetWikiQualityEvent(l.ctx, req.EventId)
	if err != nil {
		return nil, err
	}
	return &types.WikiQualityEventEnvelope{Code: 200, Msg: "success", Data: receipt}, nil
}
