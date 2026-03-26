package logic

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/user/admin/rpc/internal/metrics"
	"sea-try-go/service/user/admin/rpc/internal/model"
	"sea-try-go/service/user/admin/rpc/internal/svc"
	"sea-try-go/service/user/admin/rpc/pb"
	"sea-try-go/service/user/common/errmsg"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	inviteCodeTTL      = 10 * time.Minute
	inviteCodeMaxRetry = 3
)

type CreateInviteCodeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateInviteCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateInviteCodeLogic {
	return &CreateInviteCodeLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *CreateInviteCodeLogic) CreateInviteCode(in *pb.CreateInviteCodeReq) (*pb.CreateInviteCodeResp, error) {
	tracer := otel.Tracer("admin-rpc")
	ctx, span := tracer.Start(l.ctx, "Action-Admin-CreateInviteCode")
	defer span.End()
	span.SetAttributes(attribute.Int64("audit.operator_id", in.OperatorId))

	_, err := l.svcCtx.AdminModel.FindOneAdminByUid(ctx, in.OperatorId)
	if err != nil {
		if err == model.ErrorNotFound {
			metrics.AdminActionCount.WithLabelValues("createinvite", "user_not_found").Inc()
			logger.LogBusinessErr(ctx, errmsg.ErrorUserNotExist, err)
			return nil, status.Error(codes.NotFound, "鐢ㄦ埛涓嶅瓨鍦?")
		}
		metrics.AdminActionCount.WithLabelValues("createinvite", "db_error").Inc()
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorDbSelect, err)
		return nil, status.Error(codes.Internal, "DB鏌ヨ澶辫触")
	}

	expiresAt := time.Now().Add(inviteCodeTTL)
	for attempt := 0; attempt < inviteCodeMaxRetry; attempt++ {
		code, genErr := generateInviteCode()
		if genErr != nil {
			metrics.AdminActionCount.WithLabelValues("createinvite", "internal_error").Inc()
			span.RecordError(genErr)
			logger.LogBusinessErr(ctx, errmsg.ErrorServerCommon, genErr)
			return nil, status.Error(codes.Internal, "绠＄悊鍛橀個璇风爜鐢熸垚澶辫触")
		}

		invite := &model.AdminInvite{
			Code:       code,
			InviterUid: in.OperatorId,
			ExpiresAt:  expiresAt,
		}
		if err = l.svcCtx.AdminModel.InsertOneAdminInvite(ctx, invite); err == nil {
			metrics.AdminActionCount.WithLabelValues("createinvite", "success").Inc()
			logger.LogInfo(ctx, fmt.Sprintf("create invite code success, operator_id=%d", in.OperatorId))
			return &pb.CreateInviteCodeResp{
				InviteCode: code,
				ExpiresAt:  expiresAt.Unix(),
			}, nil
		}

		if model.IsUniqueViolation(err) {
			continue
		}

		metrics.AdminActionCount.WithLabelValues("createinvite", "db_error").Inc()
		span.RecordError(err)
		logger.LogBusinessErr(ctx, errmsg.ErrorDbInsert, err)
		return nil, status.Error(codes.Internal, "DB鍐欏叆澶辫触")
	}

	metrics.AdminActionCount.WithLabelValues("createinvite", "internal_error").Inc()
	generateErr := fmt.Errorf("invite code collision retry exceeded")
	span.RecordError(generateErr)
	logger.LogBusinessErr(ctx, errmsg.ErrorServerCommon, generateErr)
	return nil, status.Error(codes.Internal, "绠＄悊鍛橀個璇风爜鐢熸垚澶辫触")
}

func generateInviteCode() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "ADM-" + strings.ToUpper(hex.EncodeToString(buf)), nil
}
