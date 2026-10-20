package logic

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	favoritecommon "github.com/Sea-Go/Sea-RideTheWind/service/favorite/common"
	"github.com/Sea-Go/Sea-RideTheWind/service/favorite/rpc/internal/metrics"
	"github.com/Sea-Go/Sea-RideTheWind/service/favorite/rpc/internal/model"
	"github.com/Sea-Go/Sea-RideTheWind/service/favorite/rpc/internal/svc"
	favoritepb "github.com/Sea-Go/Sea-RideTheWind/service/favorite/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
)

type UpdateFavoriteFolderNameLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateFavoriteFolderNameLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateFavoriteFolderNameLogic {
	return &UpdateFavoriteFolderNameLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *UpdateFavoriteFolderNameLogic) UpdateFavoriteFolderName(in *favoritepb.UpdateFavoriteFolderNameReq) (resp *favoritepb.UpdateFavoriteFolderNameResp, err error) {
	started := time.Now()
	defer func() {
		metrics.ObserveRPC(folderModule, folderUpdateName, started, err)
	}()

	ctx, span := otel.Tracer("favorite-rpc").Start(l.ctx, "FavoriteRPC.UpdateFavoriteFolderName")
	defer span.End()

	span.SetAttributes(
		attribute.Int64("biz.user_id", in.GetUserId()),
		attribute.Int64("biz.folder_id", in.GetFolderId()),
		attribute.String("biz.folder_name", in.GetName()),
	)

	if in.GetUserId() <= 0 || in.GetFolderId() <= 0 {
		err = favoritecommon.GRPCError(codes.InvalidArgument, favoritecommon.ErrorInvalidParam)
		span.RecordError(err)
		metrics.ObserveOp(folderModule, folderUpdateName, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.BizCodeFromError(err), err, userLogOption(in.GetUserId()))
		return nil, err
	}

	name := normalizeFolderName(in.GetName())
	if name == "" {
		err = favoritecommon.GRPCError(codes.InvalidArgument, favoritecommon.ErrorFavoriteFolderNameEmpty)
		span.RecordError(err)
		metrics.ObserveOp(folderModule, folderUpdateName, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorFavoriteFolderNameEmpty, err, userLogOption(in.GetUserId()))
		return nil, err
	}

	folder, dbErr := l.svcCtx.FavoriteModel.FindFolderByFolderId(ctx, in.GetFolderId())
	if dbErr != nil {
		if errors.Is(dbErr, model.ErrorNotFound) {
			err = favoritecommon.GRPCError(codes.NotFound, favoritecommon.ErrorFavoriteFolderNotFound)
			span.RecordError(err)
			metrics.ObserveOp(folderModule, folderUpdateName, resultFail)
			logger.LogBusinessErr(ctx, favoritecommon.ErrorFavoriteFolderNotFound, err, userLogOption(in.GetUserId()))
			return nil, err
		}
		span.RecordError(dbErr)
		metrics.ObserveDBError(folderModule, "select", "db")
		metrics.ObserveOp(folderModule, folderUpdateName, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorDbSelect, dbErr, userLogOption(in.GetUserId()))
		return nil, favoritecommon.GRPCError(codes.Internal, favoritecommon.ErrorDbSelect)
	}
	if folder.UserId != in.GetUserId() {
		err = favoritecommon.GRPCError(codes.PermissionDenied, favoritecommon.ErrorForbidden)
		span.RecordError(err)
		metrics.ObserveOp(folderModule, folderUpdateName, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorForbidden, err, userLogOption(in.GetUserId()))
		return nil, err
	}

	if folder.Name == name {
		metrics.ObserveOp(folderModule, folderUpdateName, resultSuccess)
		logger.LogInfo(ctx, fmt.Sprintf("favorite folder name unchanged, folder_id=%d", in.GetFolderId()), userLogOption(in.GetUserId()))
		return &favoritepb.UpdateFavoriteFolderNameResp{Success: true}, nil
	}

	if existing, checkErr := l.svcCtx.FavoriteModel.FindFolderByUserIdAndName(ctx, in.GetUserId(), name); checkErr == nil && existing.FolderId != in.GetFolderId() {
		err = favoritecommon.GRPCError(codes.AlreadyExists, favoritecommon.ErrorFavoriteFolderNameExists)
		span.RecordError(err)
		metrics.ObserveOp(folderModule, folderUpdateName, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorFavoriteFolderNameExists, err, userLogOption(in.GetUserId()))
		return nil, err
	} else if checkErr != nil && !errors.Is(checkErr, model.ErrorNotFound) {
		span.RecordError(checkErr)
		metrics.ObserveDBError(folderModule, "select", "db")
		metrics.ObserveOp(folderModule, folderUpdateName, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorDbSelect, checkErr, userLogOption(in.GetUserId()))
		return nil, favoritecommon.GRPCError(codes.Internal, favoritecommon.ErrorDbSelect)
	}

	if dbErr = l.svcCtx.FavoriteModel.UpdateFolderNameByFolderId(ctx, in.GetFolderId(), name); dbErr != nil {
		span.RecordError(dbErr)
		metrics.ObserveOp(folderModule, folderUpdateName, resultFail)
		if isUniqueViolation(dbErr) {
			metrics.ObserveDBError(folderModule, "update", "duplicate")
			logger.LogBusinessErr(ctx, favoritecommon.ErrorFavoriteFolderNameExists, dbErr, userLogOption(in.GetUserId()))
			return nil, favoritecommon.GRPCError(codes.AlreadyExists, favoritecommon.ErrorFavoriteFolderNameExists)
		}
		metrics.ObserveDBError(folderModule, "update", "db")
		logger.LogBusinessErr(ctx, favoritecommon.ErrorDbUpdate, dbErr, userLogOption(in.GetUserId()))
		return nil, favoritecommon.GRPCError(codes.Internal, favoritecommon.ErrorDbUpdate)
	}

	metrics.ObserveOp(folderModule, folderUpdateName, resultSuccess)
	logger.LogInfo(ctx, fmt.Sprintf("favorite folder name updated, folder_id=%d", in.GetFolderId()), userLogOption(in.GetUserId()))
	return &favoritepb.UpdateFavoriteFolderNameResp{Success: true}, nil
}
