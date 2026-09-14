package logic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"sea-try-go/service/article/rpc/internal/model"
	"sea-try-go/service/article/rpc/internal/svc"
	"sea-try-go/service/article/rpc/pb"
	"sea-try-go/service/common/logger"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

var publicLoggerOnce sync.Once

func publicArticleRepo(t *testing.T) *model.ArticleRepo {
	t.Helper()
	publicLoggerOnce.Do(func() { logger.Init("article-public-test") })
	dsn := os.Getenv("ARTICLE_TEST_DSN")
	if dsn == "" {
		t.Skip("run service/article/rpc/acceptance.sh with isolated PostgreSQL 16")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schemaName := "article_public_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schemaName}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	address, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := address.Query()
	query.Set("search_path", schemaName)
	address.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(address.String()), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schemaName}.Sanitize()+" CASCADE")
		admin.Close()
	})
	if err := db.AutoMigrate(&model.Article{}); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../model/002_community_revision.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(migration)).Error; err != nil {
		t.Fatal(err)
	}
	return &model.ArticleRepo{Db: db}
}

func publicRevision(t *testing.T, repo *model.ArticleRepo, id, title, body, state string) {
	t.Helper()
	sum := sha256.Sum256([]byte(body))
	if err := repo.Db.Create(&model.ArticleRevision{
		ArticleID: id, Revision: 1, RevisionID: id + ":r1", AuthorID: "1001",
		SourceObject: id + "-r1.md", ContentSHA256: hex.EncodeToString(sum[:]),
		Title: title, Brief: "approved brief", ManualTypeTag: "approved-tag",
		SecondaryTags: model.StringArray{"approved-secondary"}, Markdown: body,
		PublishedAt: time.Unix(1000, 0).UTC(), SyncEventID: id + "-sync",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.Db.Create(&model.ArticlePublication{
		ArticleID: id, CurrentRevision: id + ":r1", PointerVersion: 1,
		State: state, LastEventID: id + "-published",
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestPublicArticleUsesApprovedRevisionAndFiltersBeforePage(t *testing.T) {
	repo := publicArticleRepo(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.Header().Set("ETag", `"test-article"`)
		if r.URL.Query().Has("location") {
			_, _ = w.Write([]byte("<LocationConstraint></LocationConstraint>"))
			return
		}
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/articles/legacy.md" {
			_, _ = w.Write([]byte("# Legacy"))
			return
		}
		if r.URL.Path == "/articles/draft.md" {
			_, _ = w.Write([]byte("# Draft source"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	endpoint := strings.TrimPrefix(server.URL, "http://")
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds: credentials.NewStaticV4("key", "secret", ""), Secure: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := &svc.ServiceContext{ArticleRepo: repo, MinioClient: minioClient}
	service.Config.MinIO.BucketName = "articles"
	created := time.Unix(1700000000, 0).UTC()
	rows := []model.Article{
		{ID: "modern", Title: "Draft title", Brief: "draft brief", Content: "draft.md",
			ManualTypeTag: "draft-tag", SecondaryTags: model.StringArray{"draft-secondary"},
			AuthorID: "1001", Status: int32(__.ArticleStatus_REVIEWING), CreatedAt: created},
		{ID: "legacy", Title: "Legacy title", Content: "legacy.md", AuthorID: "1001",
			Status: int32(__.ArticleStatus_PUBLISHED), CreatedAt: created.Add(-time.Minute)},
		{ID: "draft", Title: "Unreviewed", Content: "draft.md", AuthorID: "1001",
			Status: int32(__.ArticleStatus_DRAFT), CreatedAt: created.Add(-2 * time.Minute)},
		{ID: "withdrawn", Title: "Withdrawn", Content: "draft.md", AuthorID: "1001",
			Status: int32(__.ArticleStatus_PUBLISHED), CreatedAt: created.Add(-3 * time.Minute)},
	}
	for i := range rows {
		if err := repo.Db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	publicRevision(t, repo, "modern", "Approved title", "# Approved r1", "published")
	publicRevision(t, repo, "withdrawn", "Withdrawn r1", "# old", "retracted")
	get := NewGetArticleLogic(context.Background(), service)
	for _, id := range []string{"draft", "withdrawn", "missing"} {
		if _, err := get.GetArticle(&__.GetArticleRequest{ArticleId: id, PublicOnly: true, IncrView: true}); status.Code(err) != codes.NotFound {
			t.Fatalf("public %s should be hidden: %v", id, err)
		}
	}
	modern, err := get.GetArticle(&__.GetArticleRequest{ArticleId: "modern", PublicOnly: true, IncrView: true})
	if err != nil {
		t.Fatal(err)
	}
	if modern.Article.Status != __.ArticleStatus_PUBLISHED || modern.Article.Title != "Approved title" ||
		modern.Article.Brief != "approved brief" || modern.Article.ManualTypeTag != "approved-tag" ||
		modern.Article.MarkdownContent != "# Approved r1" || modern.Article.ExtInfo["published_revision_id"] != "modern:r1" {
		t.Fatalf("source draft leaked over approved r1: %+v", modern.Article)
	}
	withoutMinio := *service
	withoutMinio.MinioClient = nil
	if immutable, err := NewGetArticleLogic(context.Background(), &withoutMinio).GetArticle(
		&__.GetArticleRequest{ArticleId: "modern", PublicOnly: true}); err != nil || immutable.Article.MarkdownContent != "# Approved r1" {
		t.Fatalf("approved revision unexpectedly depends on MinIO: %+v %v", immutable, err)
	}
	var source model.Article
	if err := repo.Db.First(&source, "id = ?", "modern").Error; err != nil || source.Status != int32(__.ArticleStatus_REVIEWING) || source.ViewCount != 1 {
		t.Fatalf("published projection or view count: %+v %v", source, err)
	}
	legacy, err := get.GetArticle(&__.GetArticleRequest{ArticleId: "legacy", PublicOnly: true})
	if err != nil || legacy.Article.MarkdownContent != "# Legacy" || legacy.Article.ExtInfo["publication_gap"] != "legacy_revision_missing" {
		t.Fatalf("legacy link without invented revision: %+v %v", legacy, err)
	}
	list := NewListArticlesLogic(context.Background(), service)
	first, err := list.ListArticles(&__.ListArticlesRequest{PublicOnly: true, Page: 1, PageSize: 1, SortBy: "create_time", Desc: true})
	if err != nil || first.Total != 2 || len(first.Articles) != 1 || first.Articles[0].Id != "modern" ||
		first.Articles[0].Status != __.ArticleStatus_PUBLISHED || first.Articles[0].Title != "Approved title" ||
		first.Articles[0].MarkdownContent != "" {
		t.Fatalf("public page1: %+v %v", first, err)
	}
	second, err := list.ListArticles(&__.ListArticlesRequest{PublicOnly: true, Page: 2, PageSize: 1, SortBy: "create_time", Desc: true})
	if err != nil || second.Total != 2 || len(second.Articles) != 1 || second.Articles[0].Id != "legacy" {
		t.Fatalf("public page2: %+v %v", second, err)
	}
	tag := "approved-tag"
	filtered, err := list.ListArticles(&__.ListArticlesRequest{PublicOnly: true, Page: 1, PageSize: 20, ManualTypeTag: &tag})
	if err != nil || filtered.Total != 1 || len(filtered.Articles) != 1 || filtered.Articles[0].Id != "modern" {
		t.Fatalf("approved tag filter: %+v %v", filtered, err)
	}
	secondary := "approved-secondary"
	filtered, err = list.ListArticles(&__.ListArticlesRequest{PublicOnly: true, Page: 1, PageSize: 20, SecondaryTag: &secondary})
	if err != nil || filtered.Total != 1 || len(filtered.Articles) != 1 || filtered.Articles[0].Id != "modern" {
		t.Fatalf("approved secondary tag filter: %+v %v", filtered, err)
	}
	other, err := get.GetArticle(&__.GetArticleRequest{ArticleId: "modern", RequesterId: "2002"})
	if other != nil || status.Code(err) != codes.PermissionDenied {
		t.Fatalf("other author should be denied before object fetch: %+v %v", other, err)
	}
	author, err := get.GetArticle(&__.GetArticleRequest{ArticleId: "modern", RequesterId: "1001"})
	if err != nil || author.Article.MarkdownContent != "# Draft source" || author.Article.Status != __.ArticleStatus_REVIEWING {
		t.Fatalf("author must see current source draft: %+v %v", author, err)
	}
	newBody := "# Approved r2"
	newHash := sha256.Sum256([]byte(newBody))
	if err := repo.Db.Create(&model.ArticleRevision{
		ArticleID: "modern", Revision: 2, RevisionID: "modern:r2", AuthorID: "1001",
		SourceObject: "modern-r2.md", ContentSHA256: hex.EncodeToString(newHash[:]),
		Title: "Approved second title", Markdown: newBody,
		PublishedAt: time.Unix(2000, 0).UTC(), SyncEventID: "modern-sync-2",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.Db.Model(&model.ArticlePublication{}).Where("article_id = ?", "modern").
		Updates(map[string]any{"current_revision": "modern:r2", "pointer_version": 2}).Error; err != nil {
		t.Fatal(err)
	}
	secondRevision, err := get.GetArticle(&__.GetArticleRequest{ArticleId: "modern", PublicOnly: true})
	if err != nil || secondRevision.Article.MarkdownContent != newBody || secondRevision.Article.Title != "Approved second title" ||
		secondRevision.Article.ExtInfo["published_revision_id"] != "modern:r2" {
		t.Fatalf("r2 pointer did not replace r1: %+v %v", secondRevision, err)
	}
	if err := repo.Db.Model(&model.ArticlePublication{}).Where("article_id = ?", "modern").
		Updates(map[string]any{"state": "retracted", "pointer_version": 3}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := get.GetArticle(&__.GetArticleRequest{ArticleId: "modern", PublicOnly: true}); status.Code(err) != codes.NotFound {
		t.Fatalf("retracted r2 remained public: %v", err)
	}
	afterRetraction, err := list.ListArticles(&__.ListArticlesRequest{PublicOnly: true, Page: 1, PageSize: 20})
	if err != nil || afterRetraction.Total != 1 || len(afterRetraction.Articles) != 1 || afterRetraction.Articles[0].Id != "legacy" {
		t.Fatalf("retraction list leaked modern: %+v %v", afterRetraction, err)
	}
}
