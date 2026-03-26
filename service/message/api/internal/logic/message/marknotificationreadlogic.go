// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package message

import (
	"context"
	"time"

	"sea-try-go/service/message/api/internal/logic/shared"
	"sea-try-go/service/message/api/internal/metrics"
	"sea-try-go/service/message/api/internal/svc"
	"sea-try-go/service/message/api/internal/types"
	"sea-try-go/service/message/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type MarkNotificationReadLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMarkNotificationReadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MarkNotificationReadLogic {
	return &MarkNotificationReadLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MarkNotificationReadLogic) MarkNotificationRead(req *types.MarkNotificationReq) (resp *types.EmptyReq, err error) {
	started := time.Now()
	const route = "/message/v1/notifications/read"
	defer func() {
		metrics.ObserveRequest(route, started, err)
	}()

	uid, err := shared.UserIDFromContext(l.ctx)
	if err != nil {
		metrics.ObserveReject(route, "user_id_missing")
		return nil, err
	}

	_, err = l.svcCtx.MessageRpc.MarkNotificationRead(l.ctx, &pb.MarkNotificationReq{
		UserId:         uid,
		NotificationId: req.NotificationId,
	})
	if err != nil {
		return nil, shared.RPCError(err)
	}

	return &types.EmptyReq{}, nil
}
