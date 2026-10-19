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

type UpdateFavoriteFolderNameLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateFavoriteFolderNameLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateFavoriteFolderNameLogic {
	return &UpdateFavoriteFolderNameLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateFavoriteFolderNameLogic) UpdateFavoriteFolderName(req *types.UpdateFavoriteFolderNameReq) (resp *types.UpdateFavoriteFolderNameResp, code int) {
	ctx, span := otel.Tracer("favorite-api").Start(l.ctx, "FavoriteAPI.UpdateFavoriteFolderName")
	defer span.End()

	userID, err := extractUserID(ctx)
	if err != nil {
		logger.LogBusinessErr(ctx, favoritecommon.ErrorUnauthorized, err)
		return nil, favoritecommon.ErrorUnauthorized
	}

	span.SetAttributes(
		attribute.Int64("biz.user_id", userID),
		attribute.Int64("biz.folder_id", req.FolderId),
		attribute.String("biz.folder_name", req.Name),
	)

	rpcResp, rpcErr := l.svcCtx.FavoriteRpc.UpdateFavoriteFolderName(ctx, &favoriteservice.UpdateFavoriteFolderNameReq{
		UserId:   userID,
		FolderId: req.FolderId,
		Name:     req.Name,
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

	logger.LogInfo(ctx, fmt.Sprintf("update favorite folder name success, folder_id=%d", req.FolderId), userLogOption(userID))
	return &types.UpdateFavoriteFolderNameResp{Success: true}, favoritecommon.Success
}
