package logic

import (
	"context"
	"fmt"
	"time"

	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"
	favoritecommon "github.com/Sea-Go/Sea-RideTheWind/service/favorite/common"
	"github.com/Sea-Go/Sea-RideTheWind/service/favorite/rpc/internal/metrics"
	"github.com/Sea-Go/Sea-RideTheWind/service/favorite/rpc/internal/svc"
	favoritepb "github.com/Sea-Go/Sea-RideTheWind/service/favorite/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
)

type GetFavoriteFoldersLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetFavoriteFoldersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFavoriteFoldersLogic {
	return &GetFavoriteFoldersLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetFavoriteFoldersLogic) GetFavoriteFolders(in *favoritepb.GetFavoriteFoldersReq) (resp *favoritepb.GetFavoriteFoldersResp, err error) {
	started := time.Now()
	defer func() {
		metrics.ObserveRPC(folderModule, folderList, started, err)
	}()

	ctx, span := otel.Tracer("favorite-rpc").Start(l.ctx, "FavoriteRPC.GetFavoriteFolders")
	defer span.End()

	span.SetAttributes(attribute.Int64("biz.user_id", in.GetUserId()))

	if in.GetUserId() <= 0 {
		err = favoritecommon.GRPCError(codes.InvalidArgument, favoritecommon.ErrorInvalidParam)
		span.RecordError(err)
		metrics.ObserveOp(folderModule, folderList, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.BizCodeFromError(err), err)
		return nil, err
	}

	folders, dbErr := l.svcCtx.FavoriteModel.FindFoldersByUserId(ctx, in.GetUserId())
	if dbErr != nil {
		span.RecordError(dbErr)
		metrics.ObserveDBError(folderModule, "select", "db")
		metrics.ObserveOp(folderModule, folderList, resultFail)
		logger.LogBusinessErr(ctx, favoritecommon.ErrorDbSelect, dbErr, userLogOption(in.GetUserId()))
		return nil, favoritecommon.GRPCError(codes.Internal, favoritecommon.ErrorDbSelect)
	}

	pbFolders := make([]*favoritepb.FavoriteFolder, 0, len(folders))
	for _, folder := range folders {
		pbFolders = append(pbFolders, toProtoFolder(folder))
	}

	metrics.ObserveOp(folderModule, folderList, resultSuccess)
	logger.LogInfo(ctx, fmt.Sprintf("favorite folders listed, count=%d", len(pbFolders)), userLogOption(in.GetUserId()))
	return &favoritepb.GetFavoriteFoldersResp{Folders: pbFolders}, nil
}
