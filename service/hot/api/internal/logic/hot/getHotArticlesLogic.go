// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package hot

import (
	"context"
	"sync"
	"time"

	"sea-try-go/service/article/rpc/articleservice"
	articlepb "sea-try-go/service/article/rpc/pb"
	"sea-try-go/service/common/logger"
	"sea-try-go/service/hot/api/internal/metrics"
	"sea-try-go/service/hot/api/internal/svc"
	"sea-try-go/service/hot/api/internal/types"
	hotcommon "sea-try-go/service/hot/common"
	hotpb "sea-try-go/service/hot/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	getHotArticlesRoute     = "/hot/v1/articles"
	defaultPage             = int32(1)
	defaultPageSize         = int32(20)
	maxPageSize             = int32(50)
	maxHotArticles          = int32(100)
	hydrationConcurrency    = 8
	rollingScope            = "rolling"
	hotRPCCallee            = "hot_rpc_get_hot_articles"
	articleGetRPCCallee     = "article_rpc_get_article"
	skipReasonEmptyID       = "empty_article_id"
	skipReasonRPCError      = "article_rpc_error"
	skipReasonMissing       = "article_missing"
	skipReasonNotPublished  = "article_not_published"
	skipReasonNilHotPayload = "hot_rpc_nil_response"
)

type GetHotArticlesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetHotArticlesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetHotArticlesLogic {
	return &GetHotArticlesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetHotArticlesLogic) GetHotArticles(req *types.HotArticlesReq) (resp *types.HotArticlesResp, err error) {
	started := time.Now()
	defer func() {
		metrics.ObserveRequest(getHotArticlesRoute, started, err)
	}()

	page, pageSize := normalizePagination(req)

	hotResp, rpcErr := l.svcCtx.HotRpc.GetHotArticles(l.ctx, &hotpb.GetHotArticlesRequest{
		TopK: maxHotArticles,
	})
	if rpcErr != nil {
		metrics.ObserveRPCError(hotRPCCallee)
		logger.LogBusinessErr(l.ctx, hotcommon.ErrorServerCommon, rpcErr)
		return nil, hotcommon.NewErrCode(hotcommon.ErrorServerCommon)
	}
	if hotResp == nil {
		metrics.ObserveArticleSkip(skipReasonNilHotPayload)
		nilRespErr := hotcommon.NewErrCode(hotcommon.ErrorServerCommon)
		logger.LogBusinessErr(l.ctx, hotcommon.ErrorServerCommon, nilRespErr)
		return nil, nilRespErr
	}

	items := l.hydrateArticles(hotResp.GetItems())
	total := int64(len(items))
	start, end := paginate(total, page, pageSize)
	pagedItems := items[start:end]

	return &types.HotArticlesResp{
		Items:    pagedItems,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		Scope:    rollingScope,
	}, nil
}

func (l *GetHotArticlesLogic) hydrateArticles(hotItems []*hotpb.HotArticleItem) []types.HotArticleItem {
	hydrated := make([]*types.HotArticleItem, len(hotItems))
	sem := make(chan struct{}, hydrationConcurrency)
	var wg sync.WaitGroup

	for idx, hotItem := range hotItems {
		idx := idx
		hotItem := hotItem

		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() {
				<-sem
			}()

			hydrated[idx] = l.buildHotArticleItem(hotItem)
		}()
	}

	wg.Wait()

	result := make([]types.HotArticleItem, 0, len(hydrated))
	for _, item := range hydrated {
		if item == nil {
			continue
		}
		item.Rank = int32(len(result) + 1)
		result = append(result, *item)
	}

	return result
}

func (l *GetHotArticlesLogic) buildHotArticleItem(hotItem *hotpb.HotArticleItem) *types.HotArticleItem {
	if hotItem == nil || hotItem.GetArticleId() == "" {
		metrics.ObserveArticleSkip(skipReasonEmptyID)
		return nil
	}

	articleResp, rpcErr := l.svcCtx.ArticleRpc.GetArticle(l.ctx, &articleservice.GetArticleRequest{
		ArticleId: hotItem.GetArticleId(),
		IncrView:  false,
	})
	if rpcErr != nil {
		metrics.ObserveRPCError(articleGetRPCCallee)
		metrics.ObserveArticleSkip(skipReasonRPCError)
		logger.LogBusinessErr(l.ctx, hotcommon.ErrorServerCommon, rpcErr, logger.WithArticleID(hotItem.GetArticleId()))
		return nil
	}
	if articleResp == nil || articleResp.GetArticle() == nil {
		metrics.ObserveArticleSkip(skipReasonMissing)
		logger.LogInfo(l.ctx, "skip hot article because article detail is missing", logger.WithArticleID(hotItem.GetArticleId()))
		return nil
	}

	article := articleResp.GetArticle()
	if article.GetStatus() != articlepb.ArticleStatus_PUBLISHED {
		metrics.ObserveArticleSkip(skipReasonNotPublished)
		logger.LogInfo(l.ctx, "skip hot article because article is not published", logger.WithArticleID(article.GetId()))
		return nil
	}

	return &types.HotArticleItem{
		ArticleId:     article.GetId(),
		HotScore:      hotItem.GetHotScore(),
		Title:         article.GetTitle(),
		Brief:         article.GetBrief(),
		CoverImageUrl: article.GetCoverImageUrl(),
		AuthorId:      article.GetAuthorId(),
		CreateTime:    article.GetCreateTime(),
		ViewCount:     article.GetViewCount(),
		LikeCount:     article.GetLikeCount(),
		CommentCount:  article.GetCommentCount(),
		ManualTypeTag: article.GetManualTypeTag(),
		SecondaryTags: article.GetSecondaryTags(),
	}
}

func normalizePagination(req *types.HotArticlesReq) (int32, int32) {
	page := defaultPage
	pageSize := defaultPageSize

	if req != nil {
		if req.Page > 0 {
			page = req.Page
		}
		if req.PageSize > 0 {
			pageSize = req.PageSize
		}
	}

	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}

	return page, pageSize
}

func paginate(total int64, page int32, pageSize int32) (int, int) {
	if total <= 0 {
		return 0, 0
	}

	start := int((page - 1) * pageSize)
	if start < 0 || int64(start) >= total {
		return 0, 0
	}

	end := start + int(pageSize)
	if int64(end) > total {
		end = int(total)
	}

	return start, end
}
