package logic

import (
	"context"
	"testing"

	"sea-try-go/service/article/rpc/articleservice"
	articlepb "sea-try-go/service/article/rpc/pb"
	"sea-try-go/service/favorite/rpc/internal/svc"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type snapshotArticleRPC struct {
	articleservice.ArticleService
	response *articleservice.GetArticleResponse
	err      error
	request  *articleservice.GetArticleRequest
}

func (r *snapshotArticleRPC) GetArticle(_ context.Context, in *articleservice.GetArticleRequest, _ ...grpc.CallOption) (*articleservice.GetArticleResponse, error) {
	r.request = in
	return r.response, r.err
}

func TestResolveArticleSnapshotUsesPublicRevision(t *testing.T) {
	for _, test := range []struct {
		name      string
		response  *articleservice.GetArticleResponse
		err       error
		wantCode  codes.Code
		wantTitle string
		wantCover string
		wantRev   string
	}{
		{name: "published_r1_while_source_is_editing", response: &articleservice.GetArticleResponse{Article: &articleservice.Article{
			Id: "article-77", Status: articlepb.ArticleStatus_PUBLISHED, Title: "Frozen r1",
			CoverImageUrl: "r1-cover", ExtInfo: map[string]string{"published_revision_id": "article-77:r1"},
		}}, wantCode: codes.OK, wantTitle: "Frozen r1", wantCover: "r1-cover", wantRev: "article-77:r1"},
		{name: "legacy_published_without_pointer", response: &articleservice.GetArticleResponse{Article: &articleservice.Article{
			Id: "article-77", Status: articlepb.ArticleStatus_PUBLISHED, Title: "Legacy title",
			ExtInfo: map[string]string{"publication_gap": "legacy_revision_missing"},
		}}, wantCode: codes.OK, wantTitle: "Legacy title"},
		{name: "draft_returned_by_wrong_scope", response: &articleservice.GetArticleResponse{Article: &articleservice.Article{
			Id: "article-77", Status: articlepb.ArticleStatus_DRAFT, Title: "Private draft",
			ExtInfo: map[string]string{"published_revision_id": "article-77:r1"},
		}}, wantCode: codes.Unavailable},
		{name: "wrong_target", response: &articleservice.GetArticleResponse{Article: &articleservice.Article{
			Id: "article-88", Status: articlepb.ArticleStatus_PUBLISHED,
			ExtInfo: map[string]string{"published_revision_id": "article-88:r1"},
		}}, wantCode: codes.Unavailable},
		{name: "wrong_revision_target", response: &articleservice.GetArticleResponse{Article: &articleservice.Article{
			Id: "article-77", Status: articlepb.ArticleStatus_PUBLISHED,
			ExtInfo: map[string]string{"published_revision_id": "article-88:r1"},
		}}, wantCode: codes.Unavailable},
		{name: "invalid_revision", response: &articleservice.GetArticleResponse{Article: &articleservice.Article{
			Id: "article-77", Status: articlepb.ArticleStatus_PUBLISHED,
			ExtInfo: map[string]string{"published_revision_id": "article-77:r0"},
		}}, wantCode: codes.Unavailable},
		{name: "missing_publication_metadata", response: &articleservice.GetArticleResponse{Article: &articleservice.Article{
			Id: "article-77", Status: articlepb.ArticleStatus_PUBLISHED,
		}}, wantCode: codes.Unavailable},
		{name: "retracted", err: status.Error(codes.NotFound, "not public"), wantCode: codes.NotFound},
		{name: "missing", wantCode: codes.NotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &snapshotArticleRPC{response: test.response, err: test.err}
			got, err := resolveArticleSnapshot(context.Background(), &svc.ServiceContext{ArticleRpc: client}, " article-77 ")
			if status.Code(err) != test.wantCode {
				t.Fatalf("snapshot result = %+v, %v; want code %v", got, err, test.wantCode)
			}
			if client.request == nil || client.request.ArticleId != "article-77" || !client.request.PublicOnly ||
				client.request.IncrView || client.request.RequesterId != "" {
				t.Fatalf("favorite used non-public article read: %+v", client.request)
			}
			if test.wantCode == codes.OK && (got.Title != test.wantTitle || got.Cover != test.wantCover || got.RevisionID != test.wantRev) {
				t.Fatalf("snapshot = %+v; want title=%q cover=%q revision=%q", got, test.wantTitle, test.wantCover, test.wantRev)
			}
		})
	}
}
