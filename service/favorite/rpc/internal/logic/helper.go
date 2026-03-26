package logic

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"sea-try-go/service/article/rpc/articleservice"
	"sea-try-go/service/common/logger"
	favoritecommon "sea-try-go/service/favorite/common"
	"sea-try-go/service/favorite/rpc/internal/metrics"
	"sea-try-go/service/favorite/rpc/internal/model"
	"sea-try-go/service/favorite/rpc/internal/svc"
	favoritepb "sea-try-go/service/favorite/rpc/pb"
	"sea-try-go/service/user/user/rpc/userservice"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	folderModule     = "favorite_folder"
	folderCreate     = "create"
	folderDelete     = "delete"
	folderList       = "list"
	folderUpdateName = "update_name"

	itemModule       = "favorite_item"
	itemCreate       = "create"
	itemDelete       = "delete"
	itemListByFolder = "list_by_folder"

	resultSuccess = "success"
	resultFail    = "fail"
)

type articleSnapshot struct {
	Title string
	Cover string
}

func userLogOption(userID int64) logger.LogOption {
	return logger.WithUserID(strconv.FormatInt(userID, 10))
}

func articleLogOption(articleID string) logger.LogOption {
	return logger.WithArticleID(strings.TrimSpace(articleID))
}

func normalizeFolderName(name string) string {
	return strings.TrimSpace(name)
}

func normalizeTargetType(targetType string) string {
	return strings.ToLower(strings.TrimSpace(targetType))
}

func toProtoFolder(folder model.FavoriteFolder) *favoritepb.FavoriteFolder {
	return &favoritepb.FavoriteFolder{
		FolderId:  folder.FolderId,
		UserId:    folder.UserId,
		Name:      folder.Name,
		CreatedAt: folder.CreateTime.UnixMilli(),
		UpdatedAt: folder.UpdateTime.UnixMilli(),
	}
}

func toProtoItem(item model.FavoriteItem) *favoritepb.FavoriteItem {
	return &favoritepb.FavoriteItem{
		FavoriteId: item.FavoriteId,
		FolderId:   item.FolderId,
		UserId:     item.UserId,
		TargetId:   item.TargetId,
		TargetType: item.TargetType,
		Title:      item.Title,
		Cover:      item.Cover,
		CreatedAt:  item.CreateTime.UnixMilli(),
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func ensureUserExists(ctx context.Context, svcCtx *svc.ServiceContext, userID int64) error {
	depCtx, span := otel.Tracer("favorite-rpc").Start(ctx, "Dependency.User.GetUser")
	defer span.End()

	span.SetAttributes(attribute.Int64("biz.user_id", userID))

	resp, err := svcCtx.UserRpc.GetUser(depCtx, &userservice.GetUserReq{Uid: userID})
	if err != nil {
		span.RecordError(err)
		metrics.ObserveOp("dependency", "user_get", resultFail)

		st, ok := status.FromError(err)
		if ok && st.Code() == codes.NotFound {
			return favoritecommon.GRPCError(codes.NotFound, favoritecommon.ErrorUserNotFound)
		}
		return favoritecommon.GRPCError(codes.Internal, favoritecommon.ErrorServerCommon)
	}
	if resp == nil || !resp.Found {
		metrics.ObserveOp("dependency", "user_get", resultFail)
		return favoritecommon.GRPCError(codes.NotFound, favoritecommon.ErrorUserNotFound)
	}

	metrics.ObserveOp("dependency", "user_get", resultSuccess)
	return nil
}

func resolveArticleSnapshot(ctx context.Context, svcCtx *svc.ServiceContext, targetID string) (articleSnapshot, error) {
	depCtx, span := otel.Tracer("favorite-rpc").Start(ctx, "Dependency.Article.GetArticle")
	defer span.End()

	normalizedTargetID := strings.TrimSpace(targetID)
	span.SetAttributes(attribute.String("biz.article_id", normalizedTargetID))

	resp, err := svcCtx.ArticleRpc.GetArticle(depCtx, &articleservice.GetArticleRequest{
		ArticleId: normalizedTargetID,
		IncrView:  false,
	})
	if err != nil {
		span.RecordError(err)
		metrics.ObserveOp("dependency", "article_get", resultFail)

		st, ok := status.FromError(err)
		if ok && st.Code() == codes.NotFound {
			return articleSnapshot{}, favoritecommon.GRPCError(codes.NotFound, favoritecommon.ErrorNotFound)
		}
		return articleSnapshot{}, favoritecommon.GRPCError(codes.Internal, favoritecommon.ErrorServerCommon)
	}
	if resp == nil || resp.Article == nil {
		metrics.ObserveOp("dependency", "article_get", resultFail)
		return articleSnapshot{}, favoritecommon.GRPCError(codes.NotFound, favoritecommon.ErrorNotFound)
	}

	metrics.ObserveOp("dependency", "article_get", resultSuccess)
	return articleSnapshot{
		Title: strings.TrimSpace(resp.Article.Title),
		Cover: articleCover(resp.Article),
	}, nil
}

func articleCover(article *articleservice.Article) string {
	if article == nil {
		return ""
	}

	if value := strings.TrimSpace(article.CoverImageUrl); value != "" {
		return value
	}

	extra := article.GetExtInfo()
	if len(extra) == 0 {
		return ""
	}

	keys := []string{"cover", "cover_url", "coverUrl", "cover_image_url", "coverImageUrl"}
	for _, key := range keys {
		if value := strings.TrimSpace(extra[key]); value != "" {
			return value
		}
	}

	return ""
}
