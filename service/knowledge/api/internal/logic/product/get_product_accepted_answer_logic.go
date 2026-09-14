// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package product

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"
	"sea-try-go/service/user/user/identity"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetProductAcceptedAnswerLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetProductAcceptedAnswerLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetProductAcceptedAnswerLogic {
	return &GetProductAcceptedAnswerLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetProductAcceptedAnswerLogic) GetProductAcceptedAnswer(req *types.ProductAcceptedAnswerReq) (resp *types.AcceptedAnswerEnvelope, err error) {
	subject, err := identity.ResolveSubjectRef(l.ctx, l.svcCtx.UserRpc)
	if err != nil {
		return nil, err
	}
	answer, err := l.svcCtx.Store.GetAcceptedAnswer(l.ctx, types.AcceptedSubjectRef{
		AuthorityId: subject.AuthorityID, TenantId: subject.TenantID, SubjectId: subject.SubjectID,
	}, req.SessionId, req.AnswerId)
	if err != nil {
		return nil, err
	}
	return &types.AcceptedAnswerEnvelope{Code: 200, Msg: "success", Data: answer}, nil
}
