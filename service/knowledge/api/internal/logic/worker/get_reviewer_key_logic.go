// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetReviewerKeyLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetReviewerKeyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetReviewerKeyLogic {
	return &GetReviewerKeyLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetReviewerKeyLogic) GetReviewerKey(req *types.ReviewerKeyPath) (resp *types.ReviewerKeyRecordEnvelope, err error) {
	result, err := l.svcCtx.Store.GetReviewerKey(l.ctx, req.KeyId)
	if err != nil {
		return nil, err
	}
	return &types.ReviewerKeyRecordEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
