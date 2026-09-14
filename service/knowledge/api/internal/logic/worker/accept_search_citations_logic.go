// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AcceptSearchCitationsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAcceptSearchCitationsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AcceptSearchCitationsLogic {
	return &AcceptSearchCitationsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AcceptSearchCitationsLogic) AcceptSearchCitations(req *types.AcceptSearchCitationsReq) (resp *types.SearchCitationReceiptEnvelope, err error) {
	receipt, err := l.svcCtx.Store.AcceptSearchCitations(l.ctx, *req)
	if err != nil {
		return nil, err
	}
	return &types.SearchCitationReceiptEnvelope{Code: 200, Msg: "success", Data: receipt}, nil
}
