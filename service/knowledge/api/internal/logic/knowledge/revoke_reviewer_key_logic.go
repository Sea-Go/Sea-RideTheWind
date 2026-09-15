// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"
	"sea-try-go/service/knowledge/api/internal/middleware"
	"sea-try-go/service/knowledge/api/internal/model"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type RevokeReviewerKeyLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRevokeReviewerKeyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RevokeReviewerKeyLogic {
	return &RevokeReviewerKeyLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RevokeReviewerKeyLogic) RevokeReviewerKey(req *types.RevokeReviewerKeyReq) (resp *types.ReviewerKeyRecordEnvelope, err error) {
	if !l.svcCtx.Config.GroundingReviews.Enabled {
		return nil, model.ErrUnavailable
	}
	result, err := l.svcCtx.Store.RevokeReviewerKey(l.ctx, middleware.Actor(l.ctx.Value("userId")), *req)
	if err != nil {
		return nil, err
	}
	return &types.ReviewerKeyRecordEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
