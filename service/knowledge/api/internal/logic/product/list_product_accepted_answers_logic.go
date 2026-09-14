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

type ListProductAcceptedAnswersLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListProductAcceptedAnswersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListProductAcceptedAnswersLogic {
	return &ListProductAcceptedAnswersLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListProductAcceptedAnswersLogic) ListProductAcceptedAnswers(req *types.ProductAcceptedAnswersReq) (resp *types.AcceptedAnswersPageEnvelope, err error) {
	subject, err := identity.ResolveSubjectRef(l.ctx, l.svcCtx.UserRpc)
	if err != nil {
		return nil, err
	}
	page, err := l.svcCtx.Store.ListAcceptedAnswers(l.ctx, types.ListAcceptedAnswersReq{
		AuthorityId: subject.AuthorityID, TenantId: subject.TenantID,
		SubjectId: subject.SubjectID, SessionId: req.SessionId,
		AfterOrdinal: req.AfterOrdinal, Limit: req.Limit,
	})
	if err != nil {
		return nil, err
	}
	return &types.AcceptedAnswersPageEnvelope{Code: 200, Msg: "success", Data: page}, nil
}
