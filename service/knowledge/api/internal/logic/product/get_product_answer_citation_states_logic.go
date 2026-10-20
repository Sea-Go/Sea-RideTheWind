// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package product

import (
	"context"

	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/types"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/identity"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetProductAnswerCitationStatesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetProductAnswerCitationStatesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetProductAnswerCitationStatesLogic {
	return &GetProductAnswerCitationStatesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetProductAnswerCitationStatesLogic) GetProductAnswerCitationStates(req *types.ProductAcceptedAnswerReq) (resp *types.ProductAnswerCitationStatesEnvelope, err error) {
	subject, err := identity.ResolveSubjectRef(l.ctx, l.svcCtx.UserRpc)
	if err != nil {
		return nil, err
	}
	states, err := l.svcCtx.Store.GetProductAnswerCitationStates(l.ctx, types.AcceptedSubjectRef{
		AuthorityId: subject.AuthorityID, TenantId: subject.TenantID, SubjectId: subject.SubjectID,
	}, req.SessionId, req.AnswerId)
	if err != nil {
		return nil, err
	}
	return &types.ProductAnswerCitationStatesEnvelope{Code: 200, Msg: "success", Data: states}, nil
}
