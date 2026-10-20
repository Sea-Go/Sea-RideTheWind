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

type CreateFavoriteFolderLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateFavoriteFolderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateFavoriteFolderLogic {
	return &CreateFavoriteFolderLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateFavoriteFolderLogic) CreateFavoriteFolder(req *types.CreateFavoriteFolderReq) (resp *types.CreateFavoriteFolderResp, code int) {
	ctx, span := otel.Tracer("favorite-api").Start(l.ctx, "FavoriteAPI.CreateFavoriteFolder")
	defer span.End()

	userID, err := extractUserID(ctx)
	if err != nil {
		logger.LogBusinessErr(ctx, favoritecommon.ErrorUnauthorized, err)
		return nil, favoritecommon.ErrorUnauthorized
	}

	span.SetAttributes(
		attribute.Int64("biz.user_id", userID),
		attribute.String("biz.folder_name", req.Name),
	)

	rpcResp, rpcErr := l.svcCtx.FavoriteRpc.CreateFavoriteFolder(ctx, &favoriteservice.CreateFavoriteFolderReq{
		UserId: userID,
		Name:   req.Name,
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

	logger.LogInfo(ctx, fmt.Sprintf("create favorite folder success, folder_id=%d", rpcResp.FolderId), userLogOption(userID))
	return &types.CreateFavoriteFolderResp{FolderId: rpcResp.FolderId}, favoritecommon.Success
}
