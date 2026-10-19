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

type GetFavoriteFoldersLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetFavoriteFoldersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFavoriteFoldersLogic {
	return &GetFavoriteFoldersLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetFavoriteFoldersLogic) GetFavoriteFolders(req *types.GetFavoriteFoldersReq) (resp *types.GetFavoriteFoldersResp, code int) {
	ctx, span := otel.Tracer("favorite-api").Start(l.ctx, "FavoriteAPI.GetFavoriteFolders")
	defer span.End()

	userID, err := extractUserID(ctx)
	if err != nil {
		logger.LogBusinessErr(ctx, favoritecommon.ErrorUnauthorized, err)
		return nil, favoritecommon.ErrorUnauthorized
	}

	span.SetAttributes(attribute.Int64("biz.user_id", userID))

	rpcResp, rpcErr := l.svcCtx.FavoriteRpc.GetFavoriteFolders(ctx, &favoriteservice.GetFavoriteFoldersReq{UserId: userID})
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

	folders := make([]types.FavoriteFolder, 0, len(rpcResp.Folders))
	for _, folder := range rpcResp.Folders {
		folders = append(folders, types.FavoriteFolder{
			FolderId:  folder.FolderId,
			Name:      folder.Name,
			CreatedAt: folder.CreatedAt,
			UpdatedAt: folder.UpdatedAt,
		})
	}

	logger.LogInfo(ctx, fmt.Sprintf("get favorite folders success, count=%d", len(folders)), userLogOption(userID))
	return &types.GetFavoriteFoldersResp{Folders: folders}, favoritecommon.Success
}
