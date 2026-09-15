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

type GetProductAnswerCitationStatesV2Logic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetProductAnswerCitationStatesV2Logic(ctx context.Context, svcCtx *svc.ServiceContext) *GetProductAnswerCitationStatesV2Logic {
	return &GetProductAnswerCitationStatesV2Logic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetProductAnswerCitationStatesV2Logic) GetProductAnswerCitationStatesV2(req *types.ProductAcceptedAnswerReq) (resp *types.ProductAnswerCitationStatesEnvelope, err error) {
	subject, err := identity.ResolveSubjectRef(l.ctx, l.svcCtx.UserRpc)
	if err != nil {
		return nil, err
	}
	legacy := legacySubject(subject)
	if _, err = l.svcCtx.Store.GetVerifiedProductAcceptedAnswer(l.ctx, legacy, req.SessionId, req.AnswerId); err != nil {
		return nil, err
	}
	states, err := l.svcCtx.Store.GetProductAnswerCitationStates(l.ctx, legacy, req.SessionId, req.AnswerId)
	if err != nil {
		return nil, err
	}
	return &types.ProductAnswerCitationStatesEnvelope{Code: 200, Msg: "success", Data: states}, nil
}
