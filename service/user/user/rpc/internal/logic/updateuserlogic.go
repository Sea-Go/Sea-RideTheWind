package logic

import (
	"context"
	"fmt"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/cryptx"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/internal/model"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type UpdateUserLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserLogic {
	return &UpdateUserLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *UpdateUserLogic) UpdateUser(in *pb.UpdateUserReq) (*pb.UpdateUserResp, error) {

	toUpdate := &model.User{}
	if len(in.Username) > 0 {
		_, err := l.svcCtx.UserModel.FindOneByUserName(l.ctx, in.Username)
		if err == nil {
			logger.LogBusinessErr(l.ctx, errmsg.ErrorUserExist, err)
			return nil, status.Error(codes.AlreadyExists, "用户名已存在")
		}
		if err != model.ErrorNotFound {
			logger.LogBusinessErr(l.ctx, errmsg.ErrorDbSelect, err)
			return nil, status.Error(codes.Internal, "DB查询失败")
		}
		toUpdate.Username = in.Username
	}
	if len(in.Password) > 0 {
		newPassword, err := cryptx.PasswordEncrypt(in.Password)
		if err != nil {
			logger.LogBusinessErr(l.ctx, errmsg.ErrorServerCommon, err)
			return nil, status.Error(codes.Internal, "密码生成失败")
		}
		toUpdate.Password = newPassword
	}
	if len(in.Email) > 0 {
		toUpdate.Email = in.Email
	}
	if in.ExtraInfo != nil {
		toUpdate.ExtraInfo = in.ExtraInfo
	}

	err := l.svcCtx.UserModel.UpdateUserById(l.ctx, in.Uid, toUpdate)

	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorDbUpdate, err)
		return nil, status.Error(codes.Internal, "DB更新失败")
	}

	newUser, err := l.svcCtx.UserModel.FindOneByUid(l.ctx, in.Uid)

	if err != nil {
		if err == model.ErrorNotFound {
			logger.LogBusinessErr(l.ctx, errmsg.ErrorUserNotExist, err)
			return nil, status.Error(codes.NotFound, "用户不存在")
		}
		logger.LogBusinessErr(l.ctx, errmsg.ErrorDbSelect, err)
		return nil, status.Error(codes.Internal, "DB查询失败")
	}
	logger.LogInfo(l.ctx, fmt.Sprintf("update success,uid : %d", in.Uid))
	return &pb.UpdateUserResp{
		User: &pb.UserInfo{
			Uid:       newUser.Uid,
			Username:  newUser.Username,
			Email:     newUser.Email,
			ExtraInfo: newUser.ExtraInfo,
		},
	}, nil
}
