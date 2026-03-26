package article

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"sea-try-go/service/article/api/internal/svc"
	"sea-try-go/service/article/api/internal/types"
	"sea-try-go/service/article/common/errmsg"
	"sea-try-go/service/common/logger"
	"sea-try-go/service/user/user/rpc/userservice"
)

func extractCurrentUserID(ctx context.Context) (string, int) {
	uid := ctx.Value("userId")
	if uid == nil {
		return "", errmsg.ErrorUnauthorized
	}

	switch value := uid.(type) {
	case json.Number:
		if id := strings.TrimSpace(value.String()); id != "" {
			return id, errmsg.Success
		}
	case string:
		if id := strings.TrimSpace(value); id != "" {
			return id, errmsg.Success
		}
	default:
		if id := strings.TrimSpace(fmt.Sprintf("%v", uid)); id != "" && id != "<nil>" {
			return id, errmsg.Success
		}
	}

	return "", errmsg.ErrorUnauthorized
}

func enrichArticleAuthor(ctx context.Context, svcCtx *svc.ServiceContext, article *types.Article) {
	if article == nil {
		return
	}
	article.AuthorName = resolveAuthorName(ctx, svcCtx, article.AuthorId)
}

func enrichArticleAuthors(ctx context.Context, svcCtx *svc.ServiceContext, articles []types.Article) {
	if len(articles) == 0 {
		return
	}

	cache := make(map[string]string, len(articles))
	for index := range articles {
		authorID := strings.TrimSpace(articles[index].AuthorId)
		if authorID == "" {
			articles[index].AuthorName = fallbackAuthorName("")
			continue
		}
		if authorName, ok := cache[authorID]; ok {
			articles[index].AuthorName = authorName
			continue
		}

		authorName := resolveAuthorName(ctx, svcCtx, authorID)
		cache[authorID] = authorName
		articles[index].AuthorName = authorName
	}
}

func resolveAuthorName(ctx context.Context, svcCtx *svc.ServiceContext, authorID string) string {
	authorID = strings.TrimSpace(authorID)
	if authorID == "" {
		return fallbackAuthorName("")
	}

	uid, err := strconv.ParseInt(authorID, 10, 64)
	if err != nil || uid <= 0 {
		return fallbackAuthorName(authorID)
	}

	resp, rpcErr := svcCtx.UserRpc.GetUser(ctx, &userservice.GetUserReq{Uid: uid})
	if rpcErr != nil {
		logger.LogBusinessErr(ctx, errmsg.Error, rpcErr, logger.WithUserID(authorID))
		return fallbackAuthorName(authorID)
	}
	if resp == nil || !resp.Found || resp.User == nil {
		return fallbackAuthorName(authorID)
	}

	username := strings.TrimSpace(resp.User.Username)
	if username == "" {
		return fallbackAuthorName(authorID)
	}
	return username
}

func fallbackAuthorName(authorID string) string {
	if authorID = strings.TrimSpace(authorID); authorID != "" {
		return "用户 " + authorID
	}
	return "未知作者"
}

func cloneStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}

	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}
