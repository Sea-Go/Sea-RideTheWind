// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ReadSearchSourceLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewReadSearchSourceLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReadSearchSourceLogic {
	return &ReadSearchSourceLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ReadSearchSourceLogic) ReadSearchSource(req *types.ReadSearchSourceReq) (resp *types.CitationChunkEnvelope, err error) {
	chunk, err := l.svcCtx.Store.ReadSearchSource(l.ctx, *req)
	if err != nil {
		return nil, err
	}
	return &types.CitationChunkEnvelope{Code: 200, Msg: "success", Data: chunk}, nil
}
