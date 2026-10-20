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

type DeleteFavoriteFolderLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDeleteFavoriteFolderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteFavoriteFolderLogic {
	return &DeleteFavoriteFolderLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteFavoriteFolderLogic) DeleteFavoriteFolder(req *types.DeleteFavoriteFolderReq) (resp *types.DeleteFavoriteFolderResp, code int) {
	ctx, span := otel.Tracer("favorite-api").Start(l.ctx, "FavoriteAPI.DeleteFavoriteFolder")
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

	rpcResp, rpcErr := l.svcCtx.FavoriteRpc.DeleteFavoriteFolder(ctx, &favoriteservice.DeleteFavoriteFolderReq{
		UserId:   userID,
		FolderId: req.FolderId,
	})
	if rpcErr != nil {
		span.RecordError(rpcErr)
		code = codeFromRPCError(rpcErr)
		logger.LogBusinessErr(ctx, code, rpcErr, userLogOption(userID))
		return nil, code
	}
	if rpcResp == nil || !rpcResp.Success {
		err = fmt.Errorf("favorite rpc returned unsuccessful response")
		span.RecordError(err)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorServerCommon, err, userLogOption(userID))
		return nil, favoritecommon.ErrorServerCommon
	}

	logger.LogInfo(ctx, fmt.Sprintf("delete favorite folder success, folder_id=%d", req.FolderId), userLogOption(userID))
	return &types.DeleteFavoriteFolderResp{Success: true}, favoritecommon.Success
}
