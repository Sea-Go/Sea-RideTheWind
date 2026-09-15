// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package product_v2

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"
	"sea-try-go/service/user/user/identity"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetProductAcceptedAnswerV2Logic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetProductAcceptedAnswerV2Logic(ctx context.Context, svcCtx *svc.ServiceContext) *GetProductAcceptedAnswerV2Logic {
	return &GetProductAcceptedAnswerV2Logic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetProductAcceptedAnswerV2Logic) GetProductAcceptedAnswerV2(req *types.ProductAcceptedAnswerReq) (resp *types.AcceptedAnswerV2Envelope, err error) {
	subject, err := identity.ResolveSubjectRef(l.ctx, l.svcCtx.UserRpc)
	if err != nil {
		return nil, err
	}
	answer, err := l.svcCtx.Store.GetVerifiedProductAcceptedAnswer(l.ctx, legacySubject(subject), req.SessionId, req.AnswerId)
	if err != nil {
		return nil, err
	}
	return &types.AcceptedAnswerV2Envelope{Code: 200, Msg: "success", Data: projectAnswer(answer)}, nil
}
