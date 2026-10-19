package logic

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/snowflake"
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

type CreateFavoriteFolderLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateFavoriteFolderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateFavoriteFolderLogic {
	return &CreateFavoriteFolderLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *CreateFavoriteFolderLogic) CreateFavoriteFolder(in *favoritepb.CreateFavoriteFolderReq) (resp *favoritepb.CreateFavoriteFolderResp, err error) {
	started := time.Now()
	defer func() {
		metrics.ObserveRPC(folderModule, folderCreate, started, err)
	}()

	ctx, span := otel.Tracer("favorite-rpc").Start(l.ctx, "FavoriteRPC.CreateFavoriteFolder")
	defer span.End()

	span.SetAttributes(
		attribute.Int64("biz.user_id", in.GetUserId()),
		attribute.String("biz.folder_name", in.GetName()),
	)

	if in.GetUserId() <= 0 {
		err = favoritecommon.GRPCError(codes.InvalidArgument, favoritecommon.ErrorInvalidParam)
		span.RecordError(err)
		metrics.ObserveOp(folderModule, folderCreate, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.BizCodeFromError(err), err)
		return nil, err
	}

	name := normalizeFolderName(in.GetName())
	if name == "" {
		err = favoritecommon.GRPCError(codes.InvalidArgument, favoritecommon.ErrorFavoriteFolderNameEmpty)
		span.RecordError(err)
		metrics.ObserveOp(folderModule, folderCreate, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorFavoriteFolderNameEmpty, err, userLogOption(in.GetUserId()))
		return nil, err
	}

	if err = ensureUserExists(ctx, l.svcCtx, in.GetUserId()); err != nil {
		span.RecordError(err)
		metrics.ObserveOp(folderModule, folderCreate, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.BizCodeFromError(err), err, userLogOption(in.GetUserId()))
		return nil, err
	}

	if _, dbErr := l.svcCtx.FavoriteModel.FindFolderByUserIdAndName(ctx, in.GetUserId(), name); dbErr == nil {
		err = favoritecommon.GRPCError(codes.AlreadyExists, favoritecommon.ErrorFavoriteFolderNameExists)
		span.RecordError(err)
		metrics.ObserveOp(folderModule, folderCreate, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorFavoriteFolderNameExists, err, userLogOption(in.GetUserId()))
		return nil, err
	} else if !errors.Is(dbErr, model.ErrorNotFound) {
		span.RecordError(dbErr)
		metrics.ObserveDBError(folderModule, "select", "db")
		metrics.ObserveOp(folderModule, folderCreate, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorDbSelect, dbErr, userLogOption(in.GetUserId()))
		return nil, favoritecommon.GRPCError(codes.Internal, favoritecommon.ErrorDbSelect)
	}

	folderID, genErr := snowflake.GetID()
	if genErr != nil {
		span.RecordError(genErr)
		metrics.ObserveOp(folderModule, folderCreate, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorGenerateID, genErr, userLogOption(in.GetUserId()))
		return nil, favoritecommon.GRPCError(codes.Internal, favoritecommon.ErrorGenerateID)
	}

	folder := &model.FavoriteFolder{
		FolderId: folderID,
		UserId:   in.GetUserId(),
		Name:     name,
	}
	if dbErr := l.svcCtx.FavoriteModel.InsertFolder(ctx, folder); dbErr != nil {
		span.RecordError(dbErr)
		metrics.ObserveOp(folderModule, folderCreate, resultFail)
		if isUniqueViolation(dbErr) {
			metrics.ObserveDBError(folderModule, "insert", "duplicate")
			logger.LogBusinessErr(ctx, favoritecommon.ErrorFavoriteFolderNameExists, dbErr, userLogOption(in.GetUserId()))
			return nil, favoritecommon.GRPCError(codes.AlreadyExists, favoritecommon.ErrorFavoriteFolderNameExists)
		}
		metrics.ObserveDBError(folderModule, "insert", "db")
		logger.LogBusinessErr(ctx, favoritecommon.ErrorDbUpdate, dbErr, userLogOption(in.GetUserId()))
		return nil, favoritecommon.GRPCError(codes.Internal, favoritecommon.ErrorDbUpdate)
	}

	metrics.ObserveOp(folderModule, folderCreate, resultSuccess)
	logger.LogInfo(ctx, fmt.Sprintf("favorite folder created, folder_id=%d", folderID), userLogOption(in.GetUserId()))
	return &favoritepb.CreateFavoriteFolderResp{FolderId: folderID}, nil
}
