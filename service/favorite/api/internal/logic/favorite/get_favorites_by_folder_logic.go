package favorite

import (
	"context"
	"fmt"

	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	"github.com/Sea-Go/Sea-RideTheWind/service/favorite/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/favorite/api/internal/types"
	favoritecommon "github.com/Sea-Go/Sea-RideTheWind/service/favorite/common"
	"github.com/Sea-Go/Sea-RideTheWind/service/favorite/rpc/favoriteservice"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

type GetFavoritesByFolderLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetFavoritesByFolderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFavoritesByFolderLogic {
	return &GetFavoritesByFolderLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetFavoritesByFolderLogic) GetFavoritesByFolder(req *types.GetFavoritesByFolderReq) (resp *types.GetFavoritesByFolderResp, code int) {
	ctx, span := otel.Tracer("favorite-api").Start(l.ctx, "FavoriteAPI.GetFavoritesByFolder")
	defer span.End()

	userID, err := extractUserID(ctx)
	if err != nil {
		logger.LogBusinessErr(ctx, favoritecommon.ErrorUnauthorized, err)
		return nil, favoritecommon.ErrorUnauthorized
	}

	span.SetAttributes(
		attribute.Int64("biz.user_id", userID),
		attribute.Int64("biz.folder_id", req.FolderId),
	)

	rpcResp, rpcErr := l.svcCtx.FavoriteRpc.GetFavoritesByFolder(ctx, &favoriteservice.GetFavoritesByFolderReq{
		UserId:   userID,
		FolderId: req.FolderId,
	})
	if rpcErr != nil {
		span.RecordError(rpcErr)
		code = codeFromRPCError(rpcErr)
		logger.LogBusinessErr(ctx, code, rpcErr, userLogOption(userID))
		return nil, code
	}
	if rpcResp == nil {
		err = fmt.Errorf("favorite rpc returned nil response")
		span.RecordError(err)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorServerCommon, err, userLogOption(userID))
		return nil, favoritecommon.ErrorServerCommon
	}

	favorites := make([]types.FavoriteItem, 0, len(rpcResp.Favorites))
	for _, item := range rpcResp.Favorites {
		favorites = append(favorites, types.FavoriteItem{
			FavoriteId: item.FavoriteId,
			FolderId:   item.FolderId,
			TargetId:   item.TargetId,
			TargetType: item.TargetType,
			Title:      item.Title,
			Cover:      item.Cover,
			CreatedAt:  item.CreatedAt,
		})
	}

	logger.LogInfo(ctx, fmt.Sprintf("get favorites by folder success, count=%d", len(favorites)), userLogOption(userID))
	return &types.GetFavoritesByFolderResp{Favorites: favorites}, favoritecommon.Success
}
