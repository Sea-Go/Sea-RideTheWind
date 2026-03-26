package logic

import (
	"context"
	"strconv"
	"strings"

	"sea-try-go/service/article/rpc/articleservice"
	"sea-try-go/service/comment/rpc/internal/model"
)

func resolveSubjectFallback(ctx context.Context, articleRPC articleservice.ArticleService, targetType, targetID string) (model.Subject, error) {
	if !strings.EqualFold(strings.TrimSpace(targetType), "article") || articleRPC == nil {
		return model.Subject{}, model.ErrorSubjectNotFound
	}

	resp, err := articleRPC.GetArticle(ctx, &articleservice.GetArticleRequest{
		ArticleId: targetID,
		IncrView:  false,
	})
	if err != nil || resp == nil || resp.Article == nil {
		return model.Subject{}, model.ErrorSubjectNotFound
	}

	ownerID, err := strconv.ParseInt(strings.TrimSpace(resp.Article.AuthorId), 10, 64)
	if err != nil || ownerID <= 0 {
		return model.Subject{}, model.ErrorSubjectNotFound
	}

	return model.Subject{
		TargetType: targetType,
		TargetId:   targetID,
		TotalCount: 0,
		RootCount:  0,
		State:      0,
		Attribute:  0,
		OwnerId:    ownerID,
	}, nil
}
