// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetGroundingReviewLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetGroundingReviewLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetGroundingReviewLogic {
	return &GetGroundingReviewLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetGroundingReviewLogic) GetGroundingReview(req *types.GroundingReviewCasePath) (resp *types.GroundingReviewReceiptEnvelope, err error) {
	result, err := l.svcCtx.Store.GetGroundingReview(l.ctx, req.CaseSha256)
	if err != nil {
		return nil, err
	}
	return &types.GroundingReviewReceiptEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
