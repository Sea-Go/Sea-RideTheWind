package logic

import (
	"context"

	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/internal/model"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetAvatarHistoryLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetAvatarHistoryLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAvatarHistoryLogic {
	return &GetAvatarHistoryLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetAvatarHistoryLogic) GetAvatarHistory(in *pb.GetAvatarHistoryReq) (*pb.GetAvatarHistoryResp, error) {
	if _, err := l.svcCtx.UserModel.FindOneByUid(l.ctx, in.Uid); err != nil {
		if err == model.ErrorNotFound {
			logger.LogBusinessErr(l.ctx, errmsg.ErrorUserNotExist, err)
			return nil, status.Error(codes.NotFound, "鐢ㄦ埛涓嶅瓨鍦?")
		}
		logger.LogBusinessErr(l.ctx, errmsg.ErrorDbSelect, err)
		return nil, status.Error(codes.Internal, "DB鏌ヨ澶辫触")
	}

	histories, err := l.svcCtx.UserModel.ListAvatarHistory(l.ctx, in.Uid, int(in.Limit))
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorDbSelect, err)
		return nil, status.Error(codes.Internal, "DB鏌ヨ澶辫触")
	}

	items := make([]*pb.AvatarHistoryItem, 0, len(histories))
	for i := range histories {
		items = append(items, avatarHistoryItemFromModel(&histories[i]))
	}

	return &pb.GetAvatarHistoryResp{List: items}, nil
}
