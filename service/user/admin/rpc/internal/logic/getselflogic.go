package logic

import (
	"context"
	"fmt"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/user/admin/rpc/internal/model"
	"sea-try-go/service/user/admin/rpc/internal/svc"
	"sea-try-go/service/user/admin/rpc/pb"
	"sea-try-go/service/user/common/errmsg"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetSelfLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetSelfLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSelfLogic {
	return &GetSelfLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetSelfLogic) GetSelf(in *pb.GetSelfReq) (*pb.GetSelfResp, error) {
	admin, err := l.svcCtx.AdminModel.FindOneAdminByUid(l.ctx, in.Uid)
	if err != nil {
		if err == model.ErrorNotFound {
			logger.LogBusinessErr(l.ctx, errmsg.ErrorUserNotExist, err)
			return nil, status.Error(codes.NotFound, "鐢ㄦ埛涓嶅瓨鍦?")
		}
		logger.LogBusinessErr(l.ctx, errmsg.ErrorDbSelect, err)
		return nil, status.Error(codes.Internal, "DB鏌ヨ澶辫触")
	}

	logger.LogInfo(l.ctx, fmt.Sprintf("Search user success,uid : %d", in.Uid))
	return &pb.GetSelfResp{
		Admin: &pb.AdminInfo{
			Uid:       admin.Uid,
			Username:  admin.Username,
			Email:     stringValue(admin.Email),
			ExtraInfo: admin.ExtraInfo,
		},
	}, nil
}
