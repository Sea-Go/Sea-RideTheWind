package user

import (
	"context"
	"fmt"
	"strings"

	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/api/internal/types"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type SelectavatarLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSelectavatarLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SelectavatarLogic {
	return &SelectavatarLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SelectavatarLogic) Selectavatar(req *types.SelectAvatarReq) (resp *types.SelectAvatarResp, code int) {
	uid, err := currentUserID(l.ctx)
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorTokenRuntime, err)
		return nil, errmsg.ErrorTokenRuntime
	}

	if strings.TrimSpace(req.HistoryId) == "" {
		logger.LogBusinessErr(l.ctx, errmsg.Error, fmt.Errorf("history_id is empty"))
		return nil, errmsg.Error
	}

	rpcResp, err := l.svcCtx.UserRpc.SelectAvatar(l.ctx, &pb.SelectAvatarReq{
		Uid:       uid,
		HistoryId: strings.TrimSpace(req.HistoryId),
	})
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.Error, err)
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.InvalidArgument:
			return nil, errmsg.Error
		case codes.NotFound:
			return nil, errmsg.Error
		case codes.Internal:
			return nil, errmsg.ErrorServerCommon
		default:
			return nil, errmsg.CodeServerBusy
		}
	}

	return &types.SelectAvatarResp{
		AvatarUrl: rpcResp.AvatarUrl,
		History:   avatarHistoryItemFromPB(rpcResp.History),
	}, errmsg.Success
}
