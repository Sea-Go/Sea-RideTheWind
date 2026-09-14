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

type DeleteFavoriteLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteFavoriteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteFavoriteLogic {
	return &DeleteFavoriteLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *DeleteFavoriteLogic) DeleteFavorite(in *favoritepb.DeleteFavoriteReq) (resp *favoritepb.DeleteFavoriteResp, err error) {
	started := time.Now()
	defer func() {
		metrics.ObserveRPC(itemModule, itemDelete, started, err)
	}()

	ctx, span := otel.Tracer("favorite-rpc").Start(l.ctx, "FavoriteRPC.DeleteFavorite")
	defer span.End()

	span.SetAttributes(
		attribute.Int64("biz.user_id", in.GetUserId()),
		attribute.Int64("biz.favorite_id", in.GetFavoriteId()),
	)

	if in.GetUserId() <= 0 || in.GetFavoriteId() <= 0 {
		err = favoritecommon.GRPCError(codes.InvalidArgument, favoritecommon.ErrorInvalidParam)
		span.RecordError(err)
		metrics.ObserveOp(itemModule, itemDelete, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.BizCodeFromError(err), err, userLogOption(in.GetUserId()))
		return nil, err
	}

	favorite, dbErr := l.svcCtx.FavoriteModel.FindFavoriteByFavoriteId(ctx, in.GetFavoriteId())
	if dbErr != nil {
		if errors.Is(dbErr, model.ErrorNotFound) {
			err = favoritecommon.GRPCError(codes.NotFound, favoritecommon.ErrorFavoriteNotFound)
			span.RecordError(err)
			metrics.ObserveOp(itemModule, itemDelete, resultFail)
			logger.LogBusinessErr(ctx, favoritecommon.ErrorFavoriteNotFound, err, userLogOption(in.GetUserId()))
			return nil, err
		}
		span.RecordError(dbErr)
		metrics.ObserveDBError(itemModule, "select", "db")
		metrics.ObserveOp(itemModule, itemDelete, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorDbSelect, dbErr, userLogOption(in.GetUserId()))
		return nil, favoritecommon.GRPCError(codes.Internal, favoritecommon.ErrorDbSelect)
	}
	if favorite.UserId != in.GetUserId() {
		err = favoritecommon.GRPCError(codes.PermissionDenied, favoritecommon.ErrorForbidden)
		span.RecordError(err)
		metrics.ObserveOp(itemModule, itemDelete, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorForbidden, err, userLogOption(in.GetUserId()))
		return nil, err
	}

	if dbErr = l.svcCtx.FavoriteModel.DeleteFavoriteByFavoriteId(ctx, in.GetFavoriteId(), in.GetUserId()); dbErr != nil {
		span.RecordError(dbErr)
		metrics.ObserveDBError(itemModule, "delete", "db")
		metrics.ObserveOp(itemModule, itemDelete, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorDbUpdate, dbErr, userLogOption(in.GetUserId()))
		return nil, favoritecommon.GRPCError(codes.Internal, favoritecommon.ErrorDbUpdate)
	}

	metrics.ObserveOp(itemModule, itemDelete, resultSuccess)
	logger.LogInfo(ctx, fmt.Sprintf("favorite item deleted, favorite_id=%d", in.GetFavoriteId()), userLogOption(in.GetUserId()))
	return &favoritepb.DeleteFavoriteResp{Success: true}, nil
}
