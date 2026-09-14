// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetSearchCitationsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetSearchCitationsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSearchCitationsLogic {
	return &GetSearchCitationsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetSearchCitationsLogic) GetSearchCitations(req *types.SearchCitationPath) (resp *types.SearchCitationRecordEnvelope, err error) {
	record, err := l.svcCtx.Store.GetSearchCitations(l.ctx, req.SearchId)
	if err != nil {
		return nil, err
	}
	return &types.SearchCitationRecordEnvelope{Code: 200, Msg: "success", Data: record}, nil
}
