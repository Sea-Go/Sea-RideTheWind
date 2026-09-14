// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetSearchJudgmentEventLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetSearchJudgmentEventLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSearchJudgmentEventLogic {
	return &GetSearchJudgmentEventLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetSearchJudgmentEventLogic) GetSearchJudgmentEvent(req *types.SearchJudgmentEventPath) (resp *types.SearchJudgmentEventReceiptEnvelope, err error) {
	result, err := l.svcCtx.Store.GetSearchJudgmentEvent(l.ctx, req.EventId)
	if err != nil {
		return nil, err
	}
	return &types.SearchJudgmentEventReceiptEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
