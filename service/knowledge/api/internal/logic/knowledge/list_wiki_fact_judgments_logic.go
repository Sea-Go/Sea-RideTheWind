// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListWikiFactJudgmentsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListWikiFactJudgmentsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListWikiFactJudgmentsLogic {
	return &ListWikiFactJudgmentsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListWikiFactJudgmentsLogic) ListWikiFactJudgments(req *types.ListWikiFactJudgmentsReq) (resp *types.ListWikiFactJudgmentsEnvelope, err error) {
	page, err := l.svcCtx.Store.ListWikiFactJudgments(l.ctx, *req)
	if err != nil {
		return nil, err
	}
	return &types.ListWikiFactJudgmentsEnvelope{Code: 200, Msg: "success", Data: page}, nil
}
