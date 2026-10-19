package user

import (
	"context"

	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/api/internal/types"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetavatarhistoryLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetavatarhistoryLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetavatarhistoryLogic {
	return &GetavatarhistoryLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetavatarhistoryLogic) Getavatarhistory() (resp *types.AvatarHistoryResp, code int) {
	uid, err := currentUserID(l.ctx)
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorTokenRuntime, err)
		return nil, errmsg.ErrorTokenRuntime
	}

	rpcResp, err := l.svcCtx.UserRpc.GetAvatarHistory(l.ctx, &pb.GetAvatarHistoryReq{
		Uid:   uid,
		Limit: 30,
	})
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.Error, err)
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.NotFound:
			return nil, errmsg.ErrorUserNotExist
		case codes.Internal:
			return nil, errmsg.ErrorServerCommon
		default:
			return nil, errmsg.CodeServerBusy
		}
	}

	items := make([]types.AvatarHistoryItem, 0, len(rpcResp.List))
	for _, item := range rpcResp.List {
		items = append(items, avatarHistoryItemFromPB(item))
	}

	return &types.AvatarHistoryResp{List: items}, errmsg.Success
}
