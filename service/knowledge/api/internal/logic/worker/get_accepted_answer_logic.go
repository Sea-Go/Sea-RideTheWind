// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetAcceptedAnswerLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetAcceptedAnswerLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAcceptedAnswerLogic {
	return &GetAcceptedAnswerLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetAcceptedAnswerLogic) GetAcceptedAnswer(req *types.GetAcceptedAnswerReq) (resp *types.AcceptedAnswerEnvelope, err error) {
	answer, err := l.svcCtx.Store.GetAcceptedAnswer(l.ctx, types.AcceptedSubjectRef{
		AuthorityId: req.AuthorityId, TenantId: req.TenantId, SubjectId: req.SubjectId,
	}, req.SessionId, req.AnswerId)
	if err != nil {
		return nil, err
	}
	return &types.AcceptedAnswerEnvelope{Code: 200, Msg: "success", Data: answer}, nil
}
