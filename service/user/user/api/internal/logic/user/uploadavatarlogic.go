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

type UploadavatarLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUploadavatarLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UploadavatarLogic {
	return &UploadavatarLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UploadavatarLogic) Uploadavatar(req *types.UploadAvatarReq) (resp *types.UploadAvatarResp, code int) {
	uid, err := currentUserID(l.ctx)
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorTokenRuntime, err)
		return nil, errmsg.ErrorTokenRuntime
	}

	if strings.TrimSpace(req.AvatarUrl) == "" {
		logger.LogBusinessErr(l.ctx, errmsg.Error, fmt.Errorf("avatar_url is empty"))
		return nil, errmsg.Error
	}

	rpcResp, err := l.svcCtx.UserRpc.UploadAvatar(l.ctx, &pb.UploadAvatarReq{
		Uid:         uid,
		AvatarUrl:   strings.TrimSpace(req.AvatarUrl),
		ContentType: req.ContentType,
		SizeBytes:   req.SizeBytes,
	})
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.Error, err)
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.InvalidArgument:
			return nil, errmsg.Error
		case codes.NotFound:
			return nil, errmsg.ErrorUserNotExist
		case codes.Internal:
			return nil, errmsg.ErrorServerCommon
		default:
			return nil, errmsg.CodeServerBusy
		}
	}

	return &types.UploadAvatarResp{
		AvatarUrl: rpcResp.AvatarUrl,
		History:   avatarHistoryItemFromPB(rpcResp.History),
	}, errmsg.Success
}
