package logic

import (
	"context"
	"errors"
	"fmt"
	"time"

	"sea-try-go/service/common/logger"
	favoritecommon "sea-try-go/service/favorite/common"
	"sea-try-go/service/favorite/rpc/internal/metrics"
	"sea-try-go/service/favorite/rpc/internal/model"
	"sea-try-go/service/favorite/rpc/internal/svc"
	favoritepb "sea-try-go/service/favorite/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
)

type DeleteFavoriteFolderLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteFavoriteFolderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteFavoriteFolderLogic {
	return &DeleteFavoriteFolderLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *DeleteFavoriteFolderLogic) DeleteFavoriteFolder(in *favoritepb.DeleteFavoriteFolderReq) (resp *favoritepb.DeleteFavoriteFolderResp, err error) {
	started := time.Now()
	defer func() {
		metrics.ObserveRPC(folderModule, folderDelete, started, err)
	}()

	ctx, span := otel.Tracer("favorite-rpc").Start(l.ctx, "FavoriteRPC.DeleteFavoriteFolder")
	defer span.End()

	span.SetAttributes(
		attribute.Int64("biz.user_id", in.GetUserId()),
		attribute.Int64("biz.folder_id", in.GetFolderId()),
	)

	if in.GetUserId() <= 0 || in.GetFolderId() <= 0 {
		err = favoritecommon.GRPCError(codes.InvalidArgument, favoritecommon.ErrorInvalidParam)
		span.RecordError(err)
		metrics.ObserveOp(folderModule, folderDelete, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.BizCodeFromError(err), err, userLogOption(in.GetUserId()))
		return nil, err
	}

	folder, dbErr := l.svcCtx.FavoriteModel.FindFolderByFolderId(ctx, in.GetFolderId())
	if dbErr != nil {
		if errors.Is(dbErr, model.ErrorNotFound) {
			err = favoritecommon.GRPCError(codes.NotFound, favoritecommon.ErrorFavoriteFolderNotFound)
			span.RecordError(err)
			metrics.ObserveOp(folderModule, folderDelete, resultFail)
			logger.LogBusinessErr(ctx, favoritecommon.ErrorFavoriteFolderNotFound, err, userLogOption(in.GetUserId()))
			return nil, err
		}
		span.RecordError(dbErr)
		metrics.ObserveDBError(folderModule, "select", "db")
		metrics.ObserveOp(folderModule, folderDelete, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorDbSelect, dbErr, userLogOption(in.GetUserId()))
		return nil, favoritecommon.GRPCError(codes.Internal, favoritecommon.ErrorDbSelect)
	}
	if folder.UserId != in.GetUserId() {
		err = favoritecommon.GRPCError(codes.PermissionDenied, favoritecommon.ErrorForbidden)
		span.RecordError(err)
		metrics.ObserveOp(folderModule, folderDelete, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorForbidden, err, userLogOption(in.GetUserId()))
		return nil, err
	}

	if dbErr = l.svcCtx.FavoriteModel.DeleteFolderCascade(ctx, in.GetFolderId(), in.GetUserId()); dbErr != nil {
		span.RecordError(dbErr)
		metrics.ObserveDBError(folderModule, "delete", "db")
		metrics.ObserveOp(folderModule, folderDelete, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorDbUpdate, dbErr, userLogOption(in.GetUserId()))
		return nil, favoritecommon.GRPCError(codes.Internal, favoritecommon.ErrorDbUpdate)
	}

	metrics.ObserveOp(folderModule, folderDelete, resultSuccess)
	logger.LogInfo(ctx, fmt.Sprintf("favorite folder deleted, folder_id=%d", in.GetFolderId()), userLogOption(in.GetUserId()))
	return &favoritepb.DeleteFavoriteFolderResp{Success: true}, nil
}
