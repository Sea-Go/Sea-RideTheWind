package hot

import (
	"context"
	"errors"
	"testing"

	"sea-try-go/service/article/rpc/articleservice"
	articlepb "sea-try-go/service/article/rpc/pb"
	"sea-try-go/service/common/logger"
	"sea-try-go/service/hot/api/internal/svc"
	"sea-try-go/service/hot/api/internal/types"
	hotpb "sea-try-go/service/hot/rpc/pb"
)

func init() {
	logger.Init("hot-api-test")
}

type fakeHotRPC struct {
	resp *hotpb.GetHotArticlesResponse
	err  error
}

func (f fakeHotRPC) GetHotArticles(context.Context, *hotpb.GetHotArticlesRequest) (*hotpb.GetHotArticlesResponse, error) {
	return f.resp, f.err
}

type fakeArticleRPC struct {
	responses map[string]*articleservice.GetArticleResponse
	errors    map[string]error
}

func (f fakeArticleRPC) GetArticle(_ context.Context, in *articleservice.GetArticleRequest) (*articleservice.GetArticleResponse, error) {
	if err := f.errors[in.GetArticleId()]; err != nil {
		return nil, err
	}
	if resp, ok := f.responses[in.GetArticleId()]; ok {
		return resp, nil
	}
	return &articleservice.GetArticleResponse{}, nil
}

func TestGetHotArticlesAppliesDefaultsAndPageSizeCap(t *testing.T) {
	logic := NewGetHotArticlesLogic(context.Background(), &svc.ServiceContext{
		HotRpc: fakeHotRPC{resp: &hotpb.GetHotArticlesResponse{}},
		ArticleRpc: fakeArticleRPC{
			responses: map[string]*articleservice.GetArticleResponse{},
			errors:    map[string]error{},
		},
	})

	resp, err := logic.GetHotArticles(&types.HotArticlesReq{})
	if err != nil {
		t.Fatalf("GetHotArticles returned error: %v", err)
	}
	if resp.Page != 1 {
		t.Fatalf("expected default page 1, got %d", resp.Page)
	}
	if resp.PageSize != 20 {
		t.Fatalf("expected default page_size 20, got %d", resp.PageSize)
	}
	if resp.Scope != "rolling" {
		t.Fatalf("expected scope rolling, got %q", resp.Scope)
	}

	resp, err = logic.GetHotArticles(&types.HotArticlesReq{Page: 2, PageSize: 999})
	if err != nil {
		t.Fatalf("GetHotArticles returned error: %v", err)
	}
	if resp.Page != 2 {
		t.Fatalf("expected page 2, got %d", resp.Page)
	}
	if resp.PageSize != 50 {
		t.Fatalf("expected capped page_size 50, got %d", resp.PageSize)
	}
}

func TestGetHotArticlesFiltersKeepsOrderAndPaginatesAfterFiltering(t *testing.T) {
	logic := NewGetHotArticlesLogic(context.Background(), &svc.ServiceContext{
		HotRpc: fakeHotRPC{resp: &hotpb.GetHotArticlesResponse{
			Items: []*hotpb.HotArticleItem{
				{ArticleId: "a", HotScore: 99},
				{ArticleId: "b", HotScore: 88},
				{ArticleId: "c", HotScore: 77},
				{ArticleId: "d", HotScore: 66},
			},
		}},
		ArticleRpc: fakeArticleRPC{
			responses: map[string]*articleservice.GetArticleResponse{
				"a": {Article: &articlepb.Article{Id: "a", Title: "A", Status: articlepb.ArticleStatus_PUBLISHED}},
				"b": {Article: &articlepb.Article{Id: "b", Title: "B", Status: articlepb.ArticleStatus_DRAFT}},
				"c": {Article: &articlepb.Article{Id: "c", Title: "C", Status: articlepb.ArticleStatus_PUBLISHED}},
			},
			errors: map[string]error{},
		},
	})

	resp, err := logic.GetHotArticles(&types.HotArticlesReq{Page: 1, PageSize: 1})
	if err != nil {
		t.Fatalf("GetHotArticles returned error: %v", err)
	}

	if resp.Total != 2 {
		t.Fatalf("expected total 2 after filtering, got %d", resp.Total)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item on first page, got %d", len(resp.Items))
	}
	if resp.Items[0].ArticleId != "a" {
		t.Fatalf("expected first surviving article a, got %s", resp.Items[0].ArticleId)
	}
	if resp.Items[0].Rank != 1 {
		t.Fatalf("expected rank 1 for first page item, got %d", resp.Items[0].Rank)
	}

	resp, err = logic.GetHotArticles(&types.HotArticlesReq{Page: 2, PageSize: 1})
	if err != nil {
		t.Fatalf("GetHotArticles returned error: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item on second page, got %d", len(resp.Items))
	}
	if resp.Items[0].ArticleId != "c" {
		t.Fatalf("expected second surviving article c, got %s", resp.Items[0].ArticleId)
	}
	if resp.Items[0].Rank != 2 {
		t.Fatalf("expected rank 2 for second page item, got %d", resp.Items[0].Rank)
	}
}

func TestGetHotArticlesSkipsArticleRPCFailuresWithoutFailingWholeRequest(t *testing.T) {
	logic := NewGetHotArticlesLogic(context.Background(), &svc.ServiceContext{
		HotRpc: fakeHotRPC{resp: &hotpb.GetHotArticlesResponse{
			Items: []*hotpb.HotArticleItem{
				{ArticleId: "a", HotScore: 100},
				{ArticleId: "b", HotScore: 90},
			},
		}},
		ArticleRpc: fakeArticleRPC{
			responses: map[string]*articleservice.GetArticleResponse{
				"b": {Article: &articlepb.Article{Id: "b", Title: "B", Status: articlepb.ArticleStatus_PUBLISHED}},
			},
			errors: map[string]error{
				"a": errors.New("upstream failed"),
			},
		},
	})

	resp, err := logic.GetHotArticles(&types.HotArticlesReq{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("GetHotArticles returned error: %v", err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected total 1 after skipping failed hydration, got %d", resp.Total)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 surviving item, got %d", len(resp.Items))
	}
	if resp.Items[0].ArticleId != "b" {
		t.Fatalf("expected surviving article b, got %s", resp.Items[0].ArticleId)
	}
	if resp.Items[0].Rank != 1 {
		t.Fatalf("expected rank recalculated to 1, got %d", resp.Items[0].Rank)
	}
}
