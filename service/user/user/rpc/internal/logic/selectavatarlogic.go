package logic

import (
	"context"
	"strings"

	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/internal/model"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type SelectAvatarLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSelectAvatarLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SelectAvatarLogic {
	return &SelectAvatarLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *SelectAvatarLogic) SelectAvatar(in *pb.SelectAvatarReq) (*pb.SelectAvatarResp, error) {
	historyID := strings.TrimSpace(in.HistoryId)
	if historyID == "" {
		return nil, status.Error(codes.InvalidArgument, "history_id is required")
	}

	history, _, err := l.svcCtx.UserModel.SelectAvatarHistory(l.ctx, in.Uid, historyID)
	if err != nil {
		switch err {
		case model.ErrorNotFound:
			logger.LogBusinessErr(l.ctx, errmsg.ErrorUserNotExist, err)
			return nil, status.Error(codes.NotFound, "鐢ㄦ埛涓嶅瓨鍦?")
		case model.ErrorAvatarHistoryNotFound:
			logger.LogBusinessErr(l.ctx, errmsg.Error, err)
			return nil, status.Error(codes.NotFound, "澶村儚鍘嗗彶涓嶅瓨鍦?")
		default:
			logger.LogBusinessErr(l.ctx, errmsg.ErrorDbUpdate, err)
			return nil, status.Error(codes.Internal, "DB鏇存柊澶辫触")
		}
	}

	return &pb.SelectAvatarResp{
		AvatarUrl: history.AvatarURL,
		History:   avatarHistoryItemFromModel(history),
	}, nil
}
