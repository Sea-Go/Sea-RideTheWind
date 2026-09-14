package logic

import (
	"context"
	"fmt"
	"sea-try-go/service/common/logger"
	"sea-try-go/service/user/common/errmsg"
	"sea-try-go/service/user/user/rpc/internal/model"
	"sea-try-go/service/user/user/rpc/internal/svc"
	"sea-try-go/service/user/user/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetUserLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserLogic {
	return &GetUserLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetUserLogic) GetUser(in *pb.GetUserReq) (*pb.GetUserResp, error) {

	user, err := l.svcCtx.UserModel.FindOneByUid(l.ctx, in.Uid)
	if err != nil {
		if err == model.ErrorNotFound {
			logger.LogBusinessErr(l.ctx, errmsg.ErrorUserNotExist, err)
			return &pb.GetUserResp{
				Found: false,
			}, status.Error(codes.NotFound, "用户不存在")
		}
		logger.LogBusinessErr(l.ctx, errmsg.ErrorDbSelect, err)
		return &pb.GetUserResp{
			Found: false,
		}, status.Error(codes.Internal, "DB查询失败")
	}
	logger.LogInfo(l.ctx, fmt.Sprintf("search user success,uid : %d", in.Uid))
	return &pb.GetUserResp{
		User: &pb.UserInfo{
			Uid:       user.Uid,
			Username:  user.Username,
			Email:     user.Email,
			ExtraInfo: user.ExtraInfo,
			Status:    &user.Status,
		},
		Found: true,
	}, nil
}
