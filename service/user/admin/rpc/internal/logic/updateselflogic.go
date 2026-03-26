package logic

import (
	"context"
	"fmt"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/user/admin/rpc/internal/model"
	"sea-try-go/service/user/admin/rpc/internal/svc"
	"sea-try-go/service/user/admin/rpc/pb"
	"sea-try-go/service/user/common/cryptx"
	"sea-try-go/service/user/common/errmsg"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type UpdateSelfLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateSelfLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateSelfLogic {
	return &UpdateSelfLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *UpdateSelfLogic) UpdateSelf(in *pb.UpdateSelfReq) (*pb.UpdateSelfResp, error) {
	_, err := l.svcCtx.AdminModel.FindOneAdminByUid(l.ctx, in.Uid)
	if err != nil {
		if err == model.ErrorNotFound {
			return nil, status.Error(codes.NotFound, "鐢ㄦ埛涓嶅瓨鍦?")
		}
		logger.LogBusinessErr(l.ctx, errmsg.ErrorDbSelect, err)
		return nil, status.Error(codes.Internal, "DB鏌ヨ澶辫触")
	}

	if len(in.Username) > 0 {
		existAdmin, err := l.svcCtx.AdminModel.FindOneAdminByUsername(l.ctx, in.Username)
		if err == nil && existAdmin.Uid != in.Uid {
			logger.LogBusinessErr(l.ctx, errmsg.ErrorUserExist, fmt.Errorf("username %s already taken", in.Username))
			return nil, status.Error(codes.AlreadyExists, "璇ョ敤鎴峰悕宸茶浣跨敤")
		}
		if err != nil && err != model.ErrorNotFound {
			logger.LogBusinessErr(l.ctx, errmsg.ErrorDbSelect, err)
			return nil, status.Error(codes.Internal, "DB鏌ヨ澶辫触")
		}
	}

	toUpdate := &model.Admin{}
	if len(in.Username) > 0 {
		toUpdate.Username = in.Username
	}
	if len(in.Password) > 0 {
		newPassword, e := cryptx.PasswordEncrypt(in.Password)
		if e != nil {
			logger.LogBusinessErr(l.ctx, errmsg.ErrorServerCommon, e)
			return nil, status.Error(codes.Internal, "瀵嗙爜鍔犲瘑澶辫触")
		}
		toUpdate.Password = newPassword
	}
	if len(in.Email) > 0 {
		toUpdate.Email = optionalString(in.Email)
	}
	if in.ExtraInfo != nil {
		toUpdate.ExtraInfo = in.ExtraInfo
	}

	err = l.svcCtx.AdminModel.UpdateOneAdminByUid(l.ctx, in.Uid, toUpdate)
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorDbUpdate, err)
		return nil, status.Error(codes.Internal, "DB鏇存柊澶辫触")
	}

	newAdmin, err := l.svcCtx.AdminModel.FindOneAdminByUid(l.ctx, in.Uid)
	if err != nil {
		if err == model.ErrorNotFound {
			return nil, status.Error(codes.NotFound, "鐢ㄦ埛涓嶅瓨鍦?")
		}
		logger.LogBusinessErr(l.ctx, errmsg.ErrorDbSelect, err)
		return nil, status.Error(codes.Internal, "DB鏌ヨ澶辫触")
	}

	logger.LogInfo(l.ctx, fmt.Sprintf("update self success, uid : %d", in.Uid))
	return &pb.UpdateSelfResp{
		Success: true,
		Admin: &pb.AdminInfo{
			Uid:       newAdmin.Uid,
			Username:  newAdmin.Username,
			Email:     stringValue(newAdmin.Email),
			ExtraInfo: newAdmin.ExtraInfo,
		},
	}, nil
}
