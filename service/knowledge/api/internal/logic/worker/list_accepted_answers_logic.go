// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListAcceptedAnswersLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListAcceptedAnswersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListAcceptedAnswersLogic {
	return &ListAcceptedAnswersLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListAcceptedAnswersLogic) ListAcceptedAnswers(req *types.ListAcceptedAnswersReq) (resp *types.AcceptedAnswersPageEnvelope, err error) {
	page, err := l.svcCtx.Store.ListAcceptedAnswers(l.ctx, *req)
	if err != nil {
		return nil, err
	}
	return &types.AcceptedAnswersPageEnvelope{Code: 200, Msg: "success", Data: page}, nil
}
