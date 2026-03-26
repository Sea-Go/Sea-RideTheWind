package logic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/common/snowflake"
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

type CreateFavoriteLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateFavoriteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateFavoriteLogic {
	return &CreateFavoriteLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *CreateFavoriteLogic) CreateFavorite(in *favoritepb.CreateFavoriteReq) (resp *favoritepb.CreateFavoriteResp, err error) {
	started := time.Now()
	defer func() {
		metrics.ObserveRPC(itemModule, itemCreate, started, err)
	}()

	ctx, span := otel.Tracer("favorite-rpc").Start(l.ctx, "FavoriteRPC.CreateFavorite")
	defer span.End()

	span.SetAttributes(
		attribute.Int64("biz.user_id", in.GetUserId()),
		attribute.Int64("biz.folder_id", in.GetFolderId()),
		attribute.String("biz.target_id", strings.TrimSpace(in.GetTargetId())),
		attribute.String("biz.target_type", in.GetTargetType()),
	)

	if in.GetUserId() <= 0 || in.GetFolderId() <= 0 {
		err = favoritecommon.GRPCError(codes.InvalidArgument, favoritecommon.ErrorInvalidParam)
		span.RecordError(err)
		metrics.ObserveOp(itemModule, itemCreate, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.BizCodeFromError(err), err, userLogOption(in.GetUserId()))
		return nil, err
	}

	targetType := normalizeTargetType(in.GetTargetType())
	targetID := strings.TrimSpace(in.GetTargetId())
	if targetID == "" || targetType == "" {
		err = favoritecommon.GRPCError(codes.InvalidArgument, favoritecommon.ErrorFavoriteTargetEmpty)
		span.RecordError(err)
		metrics.ObserveOp(itemModule, itemCreate, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorFavoriteTargetEmpty, err, userLogOption(in.GetUserId()))
		return nil, err
	}

	if err = ensureUserExists(ctx, l.svcCtx, in.GetUserId()); err != nil {
		span.RecordError(err)
		metrics.ObserveOp(itemModule, itemCreate, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.BizCodeFromError(err), err, userLogOption(in.GetUserId()), articleLogOption(targetID))
		return nil, err
	}

	folder, dbErr := l.svcCtx.FavoriteModel.FindFolderByFolderId(ctx, in.GetFolderId())
	if dbErr != nil {
		if errors.Is(dbErr, model.ErrorNotFound) {
			err = favoritecommon.GRPCError(codes.NotFound, favoritecommon.ErrorFavoriteFolderNotFound)
			span.RecordError(err)
			metrics.ObserveOp(itemModule, itemCreate, resultFail)
			logger.LogBusinessErr(ctx, favoritecommon.ErrorFavoriteFolderNotFound, err, userLogOption(in.GetUserId()))
			return nil, err
		}
		span.RecordError(dbErr)
		metrics.ObserveDBError(folderModule, "select", "db")
		metrics.ObserveOp(itemModule, itemCreate, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorDbSelect, dbErr, userLogOption(in.GetUserId()))
		return nil, favoritecommon.GRPCError(codes.Internal, favoritecommon.ErrorDbSelect)
	}
	if folder.UserId != in.GetUserId() {
		err = favoritecommon.GRPCError(codes.PermissionDenied, favoritecommon.ErrorForbidden)
		span.RecordError(err)
		metrics.ObserveOp(itemModule, itemCreate, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorForbidden, err, userLogOption(in.GetUserId()))
		return nil, err
	}

	title := strings.TrimSpace(in.GetTitle())
	cover := strings.TrimSpace(in.GetCover())
	if targetType == "article" {
		snapshot, depErr := resolveArticleSnapshot(ctx, l.svcCtx, targetID)
		if depErr != nil {
			span.RecordError(depErr)
			metrics.ObserveOp(itemModule, itemCreate, resultFail)
			logger.LogBusinessErr(ctx, favoritecommon.BizCodeFromError(depErr), depErr, userLogOption(in.GetUserId()), articleLogOption(targetID))
			return nil, depErr
		}
		if title == "" {
			title = snapshot.Title
		}
		if cover == "" {
			cover = snapshot.Cover
		}
	}

	if _, dbErr = l.svcCtx.FavoriteModel.FindFavoriteByFolderTarget(ctx, in.GetFolderId(), targetID, targetType); dbErr == nil {
		err = favoritecommon.GRPCError(codes.AlreadyExists, favoritecommon.ErrorFavoriteAlreadyExist)
		span.RecordError(err)
		metrics.ObserveOp(itemModule, itemCreate, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorFavoriteAlreadyExist, err, userLogOption(in.GetUserId()), articleLogOption(targetID))
		return nil, err
	} else if !errors.Is(dbErr, model.ErrorNotFound) {
		span.RecordError(dbErr)
		metrics.ObserveDBError(itemModule, "select", "db")
		metrics.ObserveOp(itemModule, itemCreate, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorDbSelect, dbErr, userLogOption(in.GetUserId()), articleLogOption(targetID))
		return nil, favoritecommon.GRPCError(codes.Internal, favoritecommon.ErrorDbSelect)
	}

	favoriteID, genErr := snowflake.GetID()
	if genErr != nil {
		span.RecordError(genErr)
		metrics.ObserveOp(itemModule, itemCreate, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorGenerateID, genErr, userLogOption(in.GetUserId()), articleLogOption(targetID))
		return nil, favoritecommon.GRPCError(codes.Internal, favoritecommon.ErrorGenerateID)
	}

	favorite := &model.FavoriteItem{
		FavoriteId: favoriteID,
		FolderId:   in.GetFolderId(),
		UserId:     in.GetUserId(),
		TargetId:   targetID,
		TargetType: targetType,
		Title:      title,
		Cover:      cover,
	}
	if dbErr = l.svcCtx.FavoriteModel.InsertFavorite(ctx, favorite); dbErr != nil {
		span.RecordError(dbErr)
		metrics.ObserveOp(itemModule, itemCreate, resultFail)
		if isUniqueViolation(dbErr) {
			metrics.ObserveDBError(itemModule, "insert", "duplicate")
			logger.LogBusinessErr(ctx, favoritecommon.ErrorFavoriteAlreadyExist, dbErr, userLogOption(in.GetUserId()), articleLogOption(targetID))
			return nil, favoritecommon.GRPCError(codes.AlreadyExists, favoritecommon.ErrorFavoriteAlreadyExist)
		}
		metrics.ObserveDBError(itemModule, "insert", "db")
		logger.LogBusinessErr(ctx, favoritecommon.ErrorDbUpdate, dbErr, userLogOption(in.GetUserId()), articleLogOption(targetID))
		return nil, favoritecommon.GRPCError(codes.Internal, favoritecommon.ErrorDbUpdate)
	}

	metrics.ObserveOp(itemModule, itemCreate, resultSuccess)
	logger.LogInfo(ctx, fmt.Sprintf("favorite item created, favorite_id=%d", favoriteID), userLogOption(in.GetUserId()), articleLogOption(targetID))
	return &favoritepb.CreateFavoriteResp{FavoriteId: favoriteID}, nil
}
