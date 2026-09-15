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

type ListProductAcceptedAnswersV2Logic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListProductAcceptedAnswersV2Logic(ctx context.Context, svcCtx *svc.ServiceContext) *ListProductAcceptedAnswersV2Logic {
	return &ListProductAcceptedAnswersV2Logic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListProductAcceptedAnswersV2Logic) ListProductAcceptedAnswersV2(req *types.ProductAcceptedAnswersReq) (resp *types.AcceptedAnswersPageV2Envelope, err error) {
	subject, err := identity.ResolveSubjectRef(l.ctx, l.svcCtx.UserRpc)
	if err != nil {
		return nil, err
	}
	page, err := l.svcCtx.Store.ListVerifiedProductAcceptedAnswers(l.ctx, types.ListAcceptedAnswersReq{
		AuthorityId: subject.AuthorityID, TenantId: subject.TenantID,
		SubjectId: subject.SubjectID, SessionId: req.SessionId,
		AfterOrdinal: req.AfterOrdinal, Limit: req.Limit,
	})
	if err != nil {
		return nil, err
	}
	projected := types.AcceptedAnswersPageV2{Items: make([]types.AcceptedAnswerV2, 0, len(page.Items)), NextOrdinal: page.NextOrdinal}
	for _, item := range page.Items {
		projected.Items = append(projected.Items, projectAnswer(item))
	}
	return &types.AcceptedAnswersPageV2Envelope{Code: 200, Msg: "success", Data: projected}, nil
}
