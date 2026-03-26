package favorite

import (
	"context"
	"fmt"
	"strings"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/favorite/api/internal/svc"
	"sea-try-go/service/favorite/api/internal/types"
	favoritecommon "sea-try-go/service/favorite/common"
	"sea-try-go/service/favorite/rpc/favoriteservice"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

type CreateFavoriteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateFavoriteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateFavoriteLogic {
	return &CreateFavoriteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateFavoriteLogic) CreateFavorite(req *types.CreateFavoriteReq) (resp *types.CreateFavoriteResp, code int) {
	ctx, span := otel.Tracer("favorite-api").Start(l.ctx, "FavoriteAPI.CreateFavorite")
	defer span.End()

	userID, err := extractUserID(ctx)
	if err != nil {
		logger.LogBusinessErr(ctx, favoritecommon.ErrorUnauthorized, err)
		return nil, favoritecommon.ErrorUnauthorized
	}

	span.SetAttributes(
		attribute.Int64("biz.user_id", userID),
		attribute.Int64("biz.folder_id", req.FolderId),
		attribute.String("biz.target_id", strings.TrimSpace(req.TargetId)),
		attribute.String("biz.target_type", req.TargetType),
	)

	rpcResp, rpcErr := l.svcCtx.FavoriteRpc.CreateFavorite(ctx, &favoriteservice.CreateFavoriteReq{
		UserId:     userID,
		FolderId:   req.FolderId,
		TargetId:   req.TargetId,
		TargetType: req.TargetType,
		Title:      req.Title,
		Cover:      req.Cover,
	})
	if rpcErr != nil {
		span.RecordError(rpcErr)
		code = codeFromRPCError(rpcErr)
		logger.LogBusinessErr(ctx, code, rpcErr, userLogOption(userID), articleLogOption(req.TargetId))
		return nil, code
	}
	if rpcResp == nil {
		err = fmt.Errorf("favorite rpc returned nil response")
		span.RecordError(err)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorServerCommon, err, userLogOption(userID), articleLogOption(req.TargetId))
		return nil, favoritecommon.ErrorServerCommon
	}

	logger.LogInfo(ctx, fmt.Sprintf("create favorite success, favorite_id=%d", rpcResp.FavoriteId), userLogOption(userID), articleLogOption(req.TargetId))
	return &types.CreateFavoriteResp{FavoriteId: rpcResp.FavoriteId}, favoritecommon.Success
}
