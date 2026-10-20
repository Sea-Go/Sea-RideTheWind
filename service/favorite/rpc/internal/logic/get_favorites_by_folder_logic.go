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

type GetFavoritesByFolderLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetFavoritesByFolderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFavoritesByFolderLogic {
	return &GetFavoritesByFolderLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetFavoritesByFolderLogic) GetFavoritesByFolder(in *favoritepb.GetFavoritesByFolderReq) (resp *favoritepb.GetFavoritesByFolderResp, err error) {
	started := time.Now()
	defer func() {
		metrics.ObserveRPC(itemModule, itemListByFolder, started, err)
	}()

	ctx, span := otel.Tracer("favorite-rpc").Start(l.ctx, "FavoriteRPC.GetFavoritesByFolder")
	defer span.End()

	span.SetAttributes(
		attribute.Int64("biz.user_id", in.GetUserId()),
		attribute.Int64("biz.folder_id", in.GetFolderId()),
	)

	if in.GetUserId() <= 0 || in.GetFolderId() <= 0 {
		err = favoritecommon.GRPCError(codes.InvalidArgument, favoritecommon.ErrorInvalidParam)
		span.RecordError(err)
		metrics.ObserveOp(itemModule, itemListByFolder, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.BizCodeFromError(err), err, userLogOption(in.GetUserId()))
		return nil, err
	}

	folder, dbErr := l.svcCtx.FavoriteModel.FindFolderByFolderId(ctx, in.GetFolderId())
	if dbErr != nil {
		if errors.Is(dbErr, model.ErrorNotFound) {
			err = favoritecommon.GRPCError(codes.NotFound, favoritecommon.ErrorFavoriteFolderNotFound)
			span.RecordError(err)
			metrics.ObserveOp(itemModule, itemListByFolder, resultFail)
			logger.LogBusinessErr(ctx, favoritecommon.ErrorFavoriteFolderNotFound, err, userLogOption(in.GetUserId()))
			return nil, err
		}
		span.RecordError(dbErr)
		metrics.ObserveDBError(folderModule, "select", "db")
		metrics.ObserveOp(itemModule, itemListByFolder, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorDbSelect, dbErr, userLogOption(in.GetUserId()))
		return nil, favoritecommon.GRPCError(codes.Internal, favoritecommon.ErrorDbSelect)
	}
	if folder.UserId != in.GetUserId() {
		err = favoritecommon.GRPCError(codes.PermissionDenied, favoritecommon.ErrorForbidden)
		span.RecordError(err)
		metrics.ObserveOp(itemModule, itemListByFolder, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorForbidden, err, userLogOption(in.GetUserId()))
		return nil, err
	}

	favorites, dbErr := l.svcCtx.FavoriteModel.FindFavoritesByFolderId(ctx, in.GetFolderId())
	if dbErr != nil {
		span.RecordError(dbErr)
		metrics.ObserveDBError(itemModule, "select", "db")
		metrics.ObserveOp(itemModule, itemListByFolder, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorDbSelect, dbErr, userLogOption(in.GetUserId()))
		return nil, favoritecommon.GRPCError(codes.Internal, favoritecommon.ErrorDbSelect)
	}

	pbFavorites := make([]*favoritepb.FavoriteItem, 0, len(favorites))
	for _, favorite := range favorites {
		pbFavorites = append(pbFavorites, toProtoItem(favorite))
	}

	metrics.ObserveOp(itemModule, itemListByFolder, resultSuccess)
	logger.LogInfo(ctx, fmt.Sprintf("favorite items listed, count=%d", len(pbFavorites)), userLogOption(in.GetUserId()))
	return &favoritepb.GetFavoritesByFolderResp{Favorites: pbFavorites}, nil
}
