// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"context"

	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetWikiFactSetRevisionLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetWikiFactSetRevisionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetWikiFactSetRevisionLogic {
	return &GetWikiFactSetRevisionLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetWikiFactSetRevisionLogic) GetWikiFactSetRevision(req *types.WikiFactSetRevisionPath) (resp *types.WikiFactSetEnvelope, err error) {
	record, err := l.svcCtx.Store.GetWikiFactSetRevision(l.ctx, *req)
	if err != nil {
		return nil, err
	}
	return &types.WikiFactSetEnvelope{Code: 200, Msg: "success", Data: record}, nil
}
