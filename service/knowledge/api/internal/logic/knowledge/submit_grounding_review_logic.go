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

type SubmitGroundingReviewLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSubmitGroundingReviewLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SubmitGroundingReviewLogic {
	return &SubmitGroundingReviewLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SubmitGroundingReviewLogic) SubmitGroundingReview(req *types.SubmitGroundingReviewReq) (resp *types.GroundingReviewReceiptEnvelope, err error) {
	if !l.svcCtx.Config.GroundingReviews.Enabled {
		return nil, model.ErrUnavailable
	}
	result, err := l.svcCtx.Store.SubmitGroundingReview(l.ctx, middleware.Actor(l.ctx.Value("userId")), *req)
	if err != nil {
		return nil, err
	}
	return &types.GroundingReviewReceiptEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
