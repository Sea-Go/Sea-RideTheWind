package article

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"sea-try-go/service/article/api/internal/svc"
	"sea-try-go/service/article/rpc/articleservice"
	pb "sea-try-go/service/article/rpc/pb"
	"sea-try-go/service/common/logger"
	"sea-try-go/service/user/user/rpc/userservice"

	"github.com/golang-jwt/jwt/v4"
	"github.com/zeromicro/go-zero/rest/handler"
	"github.com/zeromicro/go-zero/rest/pathvar"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type publicReadArticleRPC struct {
	articleservice.ArticleService
	mu       sync.Mutex
	getCalls []*pb.GetArticleRequest
	listCall *pb.ListArticlesRequest
}

func (r *publicReadArticleRPC) GetArticle(_ context.Context, in *pb.GetArticleRequest, _ ...grpc.CallOption) (*pb.GetArticleResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.getCalls = append(r.getCalls, in)
	if in.ArticleId == "stale-backend" {
		return &pb.GetArticleResponse{Article: &pb.Article{
			Id: in.ArticleId, AuthorId: "1001", Status: pb.ArticleStatus_REVIEWING,
			MarkdownContent: "secret draft",
		}}, nil
	}
	if in.ArticleId == "stale-author" {
		return &pb.GetArticleResponse{Article: &pb.Article{
			Id: in.ArticleId, AuthorId: "1001", Status: pb.ArticleStatus_DRAFT,
			MarkdownContent: "secret draft",
		}}, nil
	}
	if in.RequesterId == "2002" {
		return nil, status.Error(codes.PermissionDenied, "wrong author")
	}
	if in.ArticleId == "draft" && in.PublicOnly {
		return nil, status.Error(codes.NotFound, "unpublished")
	}
	body, state := "# approved", pb.ArticleStatus_PUBLISHED
	if in.RequesterId != "" {
		body, state = "# current draft", pb.ArticleStatus_REVIEWING
	}
	return &pb.GetArticleResponse{Article: &pb.Article{
		Id: in.ArticleId, AuthorId: "1001", MarkdownContent: body, Status: state,
	}}, nil
}

func (r *publicReadArticleRPC) ListArticles(_ context.Context, in *pb.ListArticlesRequest, _ ...grpc.CallOption) (*pb.ListArticlesResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listCall = in
	if in.ManualTypeTag != nil && *in.ManualTypeTag == "old" {
		return &pb.ListArticlesResponse{Articles: []*pb.Article{{Id: "draft", Status: pb.ArticleStatus_DRAFT}}, Total: 1}, nil
	}
	return &pb.ListArticlesResponse{Articles: []*pb.Article{{Id: "published", AuthorId: "1001", Status: pb.ArticleStatus_PUBLISHED}}, Total: 1, Page: 1, PageSize: 20}, nil
}

type publicReadUserRPC struct{ userservice.UserService }

func (publicReadUserRPC) GetUser(context.Context, *userservice.GetUserReq, ...grpc.CallOption) (*userservice.GetUserResp, error) {
	return &userservice.GetUserResp{Found: false}, nil
}

func publicReadRequest(t *testing.T, h http.Handler, path, id, token string) (int, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r = pathvar.WithVars(r, map[string]string{"id": id})
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var body map[string]any
	if strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
	}
	return w.Code, body
}

func TestPublicAndAuthorHTTPReadContract(t *testing.T) {
	logger.Init("article-public-http-test")
	rpc := &publicReadArticleRPC{}
	svcCtx := &svc.ServiceContext{ArticleRpc: rpc, UserRpc: publicReadUserRPC{}}
	secret := "test-article-jwt-secret"
	svcCtx.Config.Auth.AccessSecret = secret
	tokenFor := func(uid string) string {
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"userId": uid, "exp": time.Now().Add(time.Minute).Unix(),
		}).SignedString([]byte(secret))
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	public := http.HandlerFunc(GetArticleHandler(svcCtx))
	if code, body := publicReadRequest(t, public, "/v1/article/draft?incr_view=true", "draft", ""); code != 200 || body["code"] != float64(1002) {
		t.Fatalf("draft public HTTP response: %d %+v", code, body)
	}
	if code, body := publicReadRequest(t, public, "/v1/article/published?incr_view=true", "published", ""); code != 200 || body["code"] != float64(200) {
		t.Fatalf("published public HTTP response: %d %+v", code, body)
	}
	if code, body := publicReadRequest(t, public, "/v1/article/stale-backend", "stale-backend", ""); code != 200 || body["code"] != float64(1002) || strings.Contains(bodyString(body), "secret draft") {
		t.Fatalf("old RPC leaked draft through public detail: %d %+v", code, body)
	}
	author := handler.Authorize(secret)(http.HandlerFunc(GetAuthorArticleHandler(svcCtx)))
	if code, _ := publicReadRequest(t, author, "/v1/me/article/draft", "draft", ""); code != http.StatusUnauthorized {
		t.Fatalf("author route accepted missing JWT: %d", code)
	}
	if code, body := publicReadRequest(t, author, "/v1/me/article/draft", "draft", tokenFor("2002")); code != 200 || body["code"] != float64(1006) {
		t.Fatalf("other author response: %d %+v", code, body)
	}
	if code, body := publicReadRequest(t, author, "/v1/me/article/draft", "draft", tokenFor("1001")); code != 200 || body["code"] != float64(200) {
		t.Fatalf("author draft response: %d %+v", code, body)
	}
	if code, body := publicReadRequest(t, author, "/v1/me/article/stale-author", "stale-author", tokenFor("2002")); code != 200 || body["code"] != float64(1006) || strings.Contains(bodyString(body), "secret draft") {
		t.Fatalf("old RPC leaked other author's draft: %d %+v", code, body)
	}
	if code, body := publicReadRequest(t, http.HandlerFunc(ListArticlesHandler(svcCtx)), "/v1/articles?page=1", "", ""); code != 200 || body["code"] != float64(200) {
		t.Fatalf("public list response: %d %+v", code, body)
	}
	if code, body := publicReadRequest(t, http.HandlerFunc(ListArticlesHandler(svcCtx)), "/v1/articles?manual_type_tag=old", "", ""); code != 200 || body["code"] != float64(5001) || strings.Contains(bodyString(body), "draft") {
		t.Fatalf("old RPC leaked draft through list: %d %+v", code, body)
	}
	rpc.mu.Lock()
	defer rpc.mu.Unlock()
	if len(rpc.getCalls) != 6 || !rpc.getCalls[0].PublicOnly || !rpc.getCalls[0].IncrView ||
		!rpc.getCalls[1].PublicOnly || !rpc.getCalls[2].PublicOnly || rpc.getCalls[3].RequesterId != "2002" || rpc.getCalls[3].PublicOnly ||
		rpc.getCalls[4].RequesterId != "1001" || rpc.getCalls[4].IncrView || rpc.getCalls[5].RequesterId != "2002" ||
		rpc.listCall == nil || !rpc.listCall.PublicOnly {
		t.Fatalf("HTTP to RPC scope lost: get=%+v list=%+v", rpc.getCalls, rpc.listCall)
	}
}

func bodyString(body map[string]any) string {
	encoded, _ := json.Marshal(body)
	return string(encoded)
}
