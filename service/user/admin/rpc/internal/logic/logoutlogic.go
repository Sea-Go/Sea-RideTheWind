package logic

import (
	"context"
	"fmt"

	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/rpc/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/rpc/pb"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/errmsg"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type LogoutLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewLogoutLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LogoutLogic {
	return &LogoutLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *LogoutLogic) Logout(in *pb.LogoutReq) (*pb.LogoutResp, error) {
	if in.Token == "" {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorTokenTypeWrong, fmt.Errorf("Token为空"))
		return nil, status.Error(codes.InvalidArgument, "Token不能为空")
	}
	blackListKey := fmt.Sprintf("admin:jwt_blacklist:%s", in.Token)
	expireTime := l.svcCtx.Config.AdminAuth.AccessExpire
	err := l.svcCtx.BizRedis.SetexCtx(l.ctx, blackListKey, "banned", int(expireTime))
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorRedisUpdate, err)
		return nil, status.Error(codes.Internal, "注销失败,Redis写入异常")
	}
	logger.LogInfo(l.ctx, "logout success,token blacklisted!")

	return &pb.LogoutResp{}, nil
}
