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

type RecordSearchJudgmentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRecordSearchJudgmentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RecordSearchJudgmentLogic {
	return &RecordSearchJudgmentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RecordSearchJudgmentLogic) RecordSearchJudgment(req *types.RecordSearchJudgmentReq) (resp *types.SearchJudgmentReceiptEnvelope, err error) {
	if !l.svcCtx.Config.SearchJudgments.Enabled {
		return nil, model.ErrUnavailable
	}
	result, err := l.svcCtx.Store.RecordSearchJudgment(l.ctx, middleware.Actor(l.ctx.Value("userId")), *req)
	if err != nil {
		return nil, err
	}
	return &types.SearchJudgmentReceiptEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
