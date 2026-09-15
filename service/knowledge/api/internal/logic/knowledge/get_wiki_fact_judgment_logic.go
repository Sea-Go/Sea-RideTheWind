// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetWikiFactJudgmentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetWikiFactJudgmentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetWikiFactJudgmentLogic {
	return &GetWikiFactJudgmentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetWikiFactJudgmentLogic) GetWikiFactJudgment(req *types.WikiFactJudgmentPath) (resp *types.WikiFactJudgmentEnvelope, err error) {
	record, err := l.svcCtx.Store.GetWikiFactJudgment(l.ctx, *req)
	if err != nil {
		return nil, err
	}
	return &types.WikiFactJudgmentEnvelope{Code: 200, Msg: "success", Data: record}, nil
}
