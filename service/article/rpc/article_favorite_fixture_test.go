package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"sea-try-go/service/article/rpc/internal/model"
	"sea-try-go/service/article/rpc/internal/server"
	"sea-try-go/service/article/rpc/internal/svc"
	articlepb "sea-try-go/service/article/rpc/pb"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// TestFavoriteArticleFixture is an opt-in child process for Favorite's live
// contract test. It owns a separate Article schema and exposes only a local
// control endpoint; the Favorite test consumes Article over real network gRPC.
func TestFavoriteArticleFixture(t *testing.T) {
	readyPath := os.Getenv("ARTICLE_FAVORITE_READY_FILE")
	releasePath := os.Getenv("ARTICLE_FAVORITE_RELEASE_FILE")
	dsn := os.Getenv("ARTICLE_FAVORITE_DSN")
	if readyPath == "" || releasePath == "" || dsn == "" {
		t.Skip("set ARTICLE_FAVORITE_{READY_FILE,RELEASE_FILE,DSN} for Favorite live fixture")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schemaName := "article_favorite_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schemaName}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schemaName}.Sanitize()+" CASCADE")
	}()
	address, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := address.Query()
	query.Set("search_path", schemaName)
	address.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(address.String()), &gorm.Config{NamingStrategy: schema.NamingStrategy{SingularTable: true}})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err = db.AutoMigrate(&model.Article{}); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("internal/model/002_community_revision.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Exec(string(migration)).Error; err != nil {
		t.Fatal(err)
	}
	repo := &model.ArticleRepo{Db: db}
	created := time.Unix(1700000000, 0).UTC()
	for _, article := range []model.Article{
		{ID: "article-live", Title: "source draft r2", Content: "source-r2.md", AuthorID: "1001", Status: int32(articlepb.ArticleStatus_REVIEWING), CreatedAt: created},
		{ID: "article-draft", Title: "private draft", Content: "draft.md", AuthorID: "1001", Status: int32(articlepb.ArticleStatus_DRAFT), CreatedAt: created},
		{ID: "article-withdrawn", Title: "withdrawn", Content: "withdrawn.md", AuthorID: "1001", Status: int32(articlepb.ArticleStatus_REVIEWING), CreatedAt: created},
	} {
		if err := db.Create(&article).Error; err != nil {
			t.Fatal(err)
		}
	}
	seedRevision := func(articleID string, revision int, title string) error {
		body := "# " + title
		sum := sha256.Sum256([]byte(body))
		return db.Create(&model.ArticleRevision{ArticleID: articleID, Revision: int64(revision),
			RevisionID: articleID + ":r" + strconv.Itoa(revision), AuthorID: "1001", SourceObject: articleID + ".md",
			ContentSHA256: hex.EncodeToString(sum[:]), Title: title, Markdown: body, PublishedAt: created,
			SyncEventID: articleID + "-sync-r" + strconv.Itoa(revision)}).Error
	}
	if err := seedRevision("article-live", 1, "published r1"); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ArticlePublication{ArticleID: "article-live", CurrentRevision: "article-live:r1", PointerVersion: 1, State: "published", LastEventID: "article-live-r1"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := seedRevision("article-withdrawn", 1, "withdrawn r1"); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ArticlePublication{ArticleID: "article-withdrawn", CurrentRevision: "article-withdrawn:r1", PointerVersion: 1, State: "retracted", LastEventID: "article-withdrawn"}).Error; err != nil {
		t.Fatal(err)
	}

	rpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpcServer := grpc.NewServer()
	articlepb.RegisterArticleServiceServer(rpcServer, server.NewArticleServiceServer(&svc.ServiceContext{ArticleRepo: repo}))
	go func() { _ = rpcServer.Serve(rpcListener) }()
	defer rpcServer.Stop()
	control := http.NewServeMux()
	control.HandleFunc("/publish-r2", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		if err := seedRevision("article-live", 2, "published r2"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := db.Model(&model.ArticlePublication{}).Where("article_id = ?", "article-live").Updates(map[string]any{"current_revision": "article-live:r2", "pointer_version": 2, "last_event_id": "article-live-r2"}).Error; err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	controlServer := &http.Server{Handler: control}
	controlListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = controlServer.Serve(controlListener) }()
	defer func() { _ = controlServer.Shutdown(context.Background()) }()

	ready := struct {
		ArticleAddress string `json:"article_address"`
		ControlURL     string `json:"control_url"`
	}{ArticleAddress: rpcListener.Addr().String(), ControlURL: "http://" + controlListener.Addr().String()}
	temp, err := os.CreateTemp(filepath.Dir(readyPath), ".article-favorite-ready-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := temp.Chmod(0600); err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(temp).Encode(ready); err != nil {
		t.Fatal(err)
	}
	if err := temp.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temp.Name(), readyPath); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(readyPath)
	deadline := time.Now().Add(45 * time.Second)
	for {
		if _, err := os.Stat(releasePath); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("Favorite test did not release Article fixture")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
