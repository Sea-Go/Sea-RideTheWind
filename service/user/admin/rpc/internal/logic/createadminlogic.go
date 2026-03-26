package logic

import (
	"context"
	"fmt"
	"strings"
	"time"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/common/snowflake"
	"sea-try-go/service/user/admin/rpc/internal/model"
	"sea-try-go/service/user/admin/rpc/internal/svc"
	"sea-try-go/service/user/admin/rpc/pb"
	"sea-try-go/service/user/common/cryptx"
	"sea-try-go/service/user/common/errmsg"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

type CreateAdminLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateAdminLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateAdminLogic {
	return &CreateAdminLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *CreateAdminLogic) CreateAdmin(in *pb.CreateAdminReq) (*pb.CreateAdminResp, error) {
	tracer := otel.Tracer("admin-rpc")
	ctx, span := tracer.Start(l.ctx, "Action-Admin-CreateAdmin")
	defer span.End()

	username := strings.TrimSpace(in.Username)
	inviteCode := strings.TrimSpace(in.InviteCode)
	span.SetAttributes(attribute.String("audit.admin_username", username))

	if inviteCode == "" {
		logger.LogBusinessErr(ctx, errmsg.ErrorAdminInviteCodeWrong, fmt.Errorf("invite code is empty"))
		return nil, status.Error(codes.PermissionDenied, "绠＄悊鍛橀個璇风爜閿欒")
	}

	var uid int64
	err := l.svcCtx.AdminModel.Transaction(ctx, func(tx *gorm.DB) error {
		invite, err := l.svcCtx.AdminModel.FindAdminInviteByCodeForUpdate(tx, inviteCode)
		if err != nil {
			if err == model.ErrorNotFound {
				logger.LogBusinessErr(ctx, errmsg.ErrorAdminInviteCodeWrong, fmt.Errorf("invite code not found"))
				return status.Error(codes.PermissionDenied, "绠＄悊鍛橀個璇风爜閿欒")
			}
			return err
		}

		now := time.Now()
		if invite.UsedAt != nil || !invite.ExpiresAt.After(now) {
			logger.LogBusinessErr(ctx, errmsg.ErrorAdminInviteCodeWrong, fmt.Errorf("invite code expired or used"))
			return status.Error(codes.PermissionDenied, "绠＄悊鍛橀個璇风爜閿欒")
		}

		_, err = l.svcCtx.AdminModel.FindOneAdminByUsernameTx(tx, username)
		if err == nil {
			logger.LogBusinessErr(ctx, errmsg.ErrorUserExist, fmt.Errorf("username has existed"))
			return status.Error(codes.AlreadyExists, "鐢ㄦ埛鍚嶅凡瀛樺湪")
		}
		if err != model.ErrorNotFound {
			return err
		}

		password, err := cryptx.PasswordEncrypt(in.Password)
		if err != nil {
			return err
		}

		uid, err = snowflake.GetID()
		if err != nil {
			return err
		}

		admin := &model.Admin{
			Uid:       uid,
			Username:  username,
			Password:  password,
			Email:     optionalString(in.Email),
			ExtraInfo: in.ExtraInfo,
		}
		if err = l.svcCtx.AdminModel.InsertOneAdminTx(tx, admin); err != nil {
			if model.IsUniqueViolation(err) {
				logger.LogBusinessErr(ctx, errmsg.ErrorUserExist, err)
				return status.Error(codes.AlreadyExists, "鐢ㄦ埛鍚嶅凡瀛樺湪")
			}
			return err
		}

		usedAt := time.Now()
		if err = l.svcCtx.AdminModel.UpdateAdminInviteUsedTx(tx, invite.Id, uid, usedAt); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		span.RecordError(err)
		if st, ok := status.FromError(err); ok {
			switch st.Code() {
			case codes.PermissionDenied, codes.AlreadyExists:
				return nil, err
			}
		}
		logger.LogBusinessErr(ctx, errmsg.ErrorDbUpdate, err)
		return nil, status.Error(codes.Internal, "DB娣诲姞澶辫触")
	}

	logger.LogInfo(ctx, fmt.Sprintf("add admin success,uid: %d", uid))
	return &pb.CreateAdminResp{
		Uid: uid,
	}, nil
}
