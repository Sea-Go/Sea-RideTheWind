package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sea-try-go/service/article/rpc/internal/model"
	"sea-try-go/service/article/rpc/internal/server"
	"sea-try-go/service/article/rpc/internal/svc"
	articlepb "sea-try-go/service/article/rpc/pb"
	"sea-try-go/service/common/logger"
	userjwt "sea-try-go/service/user/common/jwt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"google.golang.org/grpc"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

type articleHTTPResponse struct {
	Code int `json:"code"`
	Data struct {
		Article struct {
			ID      string            `json:"id"`
			Title   string            `json:"title"`
			Content string            `json:"content"`
			Status  int32             `json:"status"`
			ExtInfo map[string]string `json:"ext_info"`
		} `json:"article"`
		Articles []json.RawMessage `json:"articles"`
		Total    int               `json:"total"`
	} `json:"data"`
}

// TestArticleHTTPChain crosses the real go-zero HTTP route, a network gRPC
// ArticleServiceServer, PostgreSQL 16, and an S3-compatible source fixture.
// Review/Kafka delivery and the production Article RPC process are separate.
func TestArticleHTTPChain(t *testing.T) {
	dsn := os.Getenv("ARTICLE_TEST_DSN")
	if dsn == "" {
		t.Skip("set ARTICLE_TEST_DSN with an isolated PostgreSQL 16 instance")
	}
	logger.Init("article-http-chain-test")
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schemaName := "article_http_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schemaName}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schemaName}.Sanitize()+" CASCADE")
	}()
	dbURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := dbURL.Query()
	query.Set("search_path", schemaName)
	dbURL.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(dbURL.String()), &gorm.Config{NamingStrategy: schema.NamingStrategy{SingularTable: true}})
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
	for _, row := range []model.Article{
		{ID: "live-modern", Title: "待审 r2", Content: "modern-r2.md", AuthorID: "1001", Status: int32(articlepb.ArticleStatus_REVIEWING), CreatedAt: created},
		{ID: "live-legacy", Title: "旧文章", Content: "legacy.md", AuthorID: "1001", Status: int32(articlepb.ArticleStatus_PUBLISHED), CreatedAt: created.Add(-time.Minute)},
		{ID: "live-hidden", Title: "未公开", Content: "modern-r2.md", AuthorID: "1001", Status: int32(articlepb.ArticleStatus_DRAFT), CreatedAt: created.Add(-2 * time.Minute)},
	} {
		if err = db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	r1 := "# 审核通过 r1"
	sum := sha256.Sum256([]byte(r1))
	if err = db.Create(&model.ArticleRevision{
		ArticleID: "live-modern", Revision: 1, RevisionID: "live-modern:r1", AuthorID: "1001",
		SourceObject: "modern-r1.md", ContentSHA256: hex.EncodeToString(sum[:]), Title: "公开 r1",
		Markdown: r1, PublishedAt: created, SyncEventID: "live-modern-sync-r1",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&model.ArticlePublication{
		ArticleID: "live-modern", CurrentRevision: "live-modern:r1", PointerVersion: 1,
		State: "published", LastEventID: "live-modern-published-r1",
	}).Error; err != nil {
		t.Fatal(err)
	}
	s3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.Header().Set("ETag", `"article-http-chain"`)
		if r.URL.Query().Has("location") {
			_, _ = w.Write([]byte("<LocationConstraint></LocationConstraint>"))
			return
		}
		if r.Method == http.MethodHead {
			return
		}
		switch r.URL.Path {
		case "/articles/modern-r2.md":
			_, _ = w.Write([]byte("# 作者待审 r2"))
		case "/articles/legacy.md":
			_, _ = w.Write([]byte("# 旧对象内容"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer s3.Close()
	minioClient, err := minio.New(strings.TrimPrefix(s3.URL, "http://"), &minio.Options{
		Creds: credentials.NewStaticV4("local-key", "local-secret", ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	rpcCtx := &svc.ServiceContext{ArticleRepo: repo, MinioClient: minioClient}
	rpcCtx.Config.MinIO.BucketName = "articles"
	rpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpcServer := grpc.NewServer()
	articlepb.RegisterArticleServiceServer(rpcServer, server.NewArticleServiceServer(rpcCtx))
	go func() { _ = rpcServer.Serve(rpcListener) }()
	defer rpcServer.Stop()

	apiListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	apiPort := apiListener.Addr().(*net.TCPAddr).Port
	_ = apiListener.Close()
	const jwtSecret = "article-http-chain-local-only"
	config := fmt.Sprintf("Name: article-http-chain\nHost: 127.0.0.1\nPort: %d\nArticleRpcConf:\n  Endpoints: [%q]\nSecurityRpcConf:\n  Endpoints: [%q]\nUserRpcConf:\n  Endpoints: [%q]\nAuth:\n  AccessSecret: %q\n  AccessExpire: 3600\nLog:\n  ServiceName: article-http-chain\n  Mode: console\n", apiPort, rpcListener.Addr().String(), rpcListener.Addr().String(), rpcListener.Addr().String(), jwtSecret)
	workDir := t.TempDir()
	configPath := filepath.Join(workDir, "article-api.yaml")
	if err = os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean("../../..")
	binary := filepath.Join(workDir, "article-api")
	build := exec.Command("go", "build", "-mod=readonly", "-p=2", "-o", binary, "./service/article/api")
	build.Dir = root
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("build Article API: %v\n%s", buildErr, output)
	}
	apiLog, err := os.Create(filepath.Join(workDir, "article-api.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer apiLog.Close()
	api := exec.Command(binary, "-f", configPath)
	api.Stdout, api.Stderr = apiLog, apiLog
	if err = api.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = api.Process.Kill()
		_ = api.Wait()
	}()
	base := fmt.Sprintf("http://127.0.0.1:%d", apiPort)
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(15 * time.Second)
	for {
		resp, requestErr := client.Get(base + "/v1/article/live-hidden")
		if requestErr == nil {
			_ = resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			logBytes, _ := os.ReadFile(filepath.Join(workDir, "article-api.log"))
			t.Fatalf("Article API not ready: %v\n%s", requestErr, logBytes)
		}
		time.Sleep(100 * time.Millisecond)
	}
	owner, err := userjwt.GetToken(jwtSecret, time.Now().Unix(), 3600, 1001)
	if err != nil {
		t.Fatal(err)
	}
	other, err := userjwt.GetToken(jwtSecret, time.Now().Unix(), 3600, 2002)
	if err != nil {
		t.Fatal(err)
	}
	read := func(label, baseURL, path, token string, wantHTTP, wantCode int, wantContent string) {
		t.Helper()
		request, requestErr := http.NewRequest(http.MethodGet, baseURL+path, nil)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response, requestErr := client.Do(request)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		defer response.Body.Close()
		if wantHTTP != http.StatusOK {
			body, bodyErr := io.ReadAll(response.Body)
			if bodyErr != nil {
				t.Fatal(bodyErr)
			}
			if response.StatusCode != wantHTTP {
				t.Fatalf("%s: HTTP=%d body=%q", label, response.StatusCode, body)
			}
			t.Logf("%s: HTTP=%d body=%q", label, response.StatusCode, body)
			return
		}
		var payload articleHTTPResponse
		if decodeErr := json.NewDecoder(response.Body).Decode(&payload); decodeErr != nil {
			t.Fatalf("%s decode: %v", label, decodeErr)
		}
		if response.StatusCode != wantHTTP || payload.Code != wantCode || payload.Data.Article.Content != wantContent {
			t.Fatalf("%s: HTTP=%d code=%d article=%+v", label, response.StatusCode, payload.Code, payload.Data.Article)
		}
		t.Logf("%s: HTTP=%d code=%d article_id=%s title=%q content=%q revision=%q", label,
			response.StatusCode, payload.Code, payload.Data.Article.ID, payload.Data.Article.Title,
			payload.Data.Article.Content, payload.Data.Article.ExtInfo["published_revision_id"])
	}
	read("public-r1", base, "/v1/article/live-modern?incr_view=false", "", 200, 200, r1)
	read("owner-draft-r2", base, "/v1/me/article/live-modern", owner, 200, 200, "# 作者待审 r2")
	read("other-denied", base, "/v1/me/article/live-modern", other, 200, 1006, "")
	read("anonymous-denied", base, "/v1/me/article/live-modern", "", 401, 0, "")
	read("legacy-object", base, "/v1/article/live-legacy", "", 200, 200, "# 旧对象内容")
	read("draft-hidden", base, "/v1/article/live-hidden", "", 200, 1002, "")

	webBase := ""
	if webRoot := os.Getenv("ARTICLE_WEB_ROOT"); webRoot != "" {
		webListener, listenErr := net.Listen("tcp", "127.0.0.1:0")
		if listenErr != nil {
			t.Fatal(listenErr)
		}
		webPort := webListener.Addr().(*net.TCPAddr).Port
		_ = webListener.Close()
		webLogPath := filepath.Join(workDir, "next.log")
		webLog, createErr := os.Create(webLogPath)
		if createErr != nil {
			t.Fatal(createErr)
		}
		defer webLog.Close()
		web := exec.Command("bun", "run", "dev", "--hostname", "127.0.0.1", "--port", fmt.Sprint(webPort))
		web.Dir = webRoot
		web.Env = append(os.Environ(), "ARTICLE_API_SERVER_URL="+base, "NEXT_TELEMETRY_DISABLED=1")
		web.Stdout, web.Stderr = webLog, webLog
		if startErr := web.Start(); startErr != nil {
			t.Fatal(startErr)
		}
		defer func() {
			_ = web.Process.Kill()
			_ = web.Wait()
		}()
		webBase = fmt.Sprintf("http://127.0.0.1:%d", webPort)
		webDeadline := time.Now().Add(90 * time.Second)
		for {
			response, requestErr := client.Get(webBase + "/api/article/v1/article/live-hidden")
			if requestErr == nil && response.StatusCode == http.StatusOK {
				_ = response.Body.Close()
				break
			}
			if response != nil {
				_ = response.Body.Close()
			}
			if time.Now().After(webDeadline) {
				logBytes, _ := os.ReadFile(webLogPath)
				t.Fatalf("Web Next BFF not ready: %v\n%s", requestErr, logBytes)
			}
			time.Sleep(200 * time.Millisecond)
		}
		read("web-public-r1", webBase, "/api/article/v1/article/live-modern?incr_view=false", "", 200, 200, r1)
		read("web-owner-draft-r2", webBase, "/api/article/v1/me/article/live-modern", owner, 200, 200, "# 作者待审 r2")
		read("web-other-denied", webBase, "/api/article/v1/me/article/live-modern", other, 200, 1006, "")
		read("web-anonymous-denied", webBase, "/api/article/v1/me/article/live-modern", "", 401, 0, "")
		read("web-legacy-object", webBase, "/api/article/v1/article/live-legacy", "", 200, 200, "# 旧对象内容")
	}
	if err = db.Model(&model.ArticlePublication{}).Where("article_id = ?", "live-modern").
		Updates(map[string]any{"state": "retracted", "pointer_version": 2}).Error; err != nil {
		t.Fatal(err)
	}
	read("withdrawn-hidden", base, "/v1/article/live-modern", "", 200, 1002, "")
	read("owner-after-withdrawal", base, "/v1/me/article/live-modern", owner, 200, 200, "# 作者待审 r2")
	if webBase != "" {
		read("web-withdrawn-hidden", webBase, "/api/article/v1/article/live-modern", "", 200, 1002, "")
		read("web-owner-after-withdrawal", webBase, "/api/article/v1/me/article/live-modern", owner, 200, 200, "# 作者待审 r2")
		if err = api.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		_ = api.Wait()
		outage, requestErr := client.Get(webBase + "/api/article/v1/article/live-legacy")
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		var unavailable struct {
			Code int `json:"code"`
		}
		decodeErr := json.NewDecoder(outage.Body).Decode(&unavailable)
		_ = outage.Body.Close()
		if decodeErr != nil || outage.StatusCode != 502 || unavailable.Code != 502 {
			t.Fatalf("Web BFF upstream outage: HTTP=%d code=%d decode=%v", outage.StatusCode, unavailable.Code, decodeErr)
		}
		t.Logf("web-upstream-outage: HTTP=%d code=%d", outage.StatusCode, unavailable.Code)
		api = exec.Command(binary, "-f", configPath)
		api.Stdout, api.Stderr = apiLog, apiLog
		if err = api.Start(); err != nil {
			t.Fatal(err)
		}
		recoveryDeadline := time.Now().Add(15 * time.Second)
		for {
			response, requestErr := client.Get(base + "/v1/article/live-legacy")
			if requestErr == nil {
				_ = response.Body.Close()
				break
			}
			if time.Now().After(recoveryDeadline) {
				t.Fatalf("Article API did not recover: %v", requestErr)
			}
			time.Sleep(100 * time.Millisecond)
		}
		read("web-upstream-recovered", webBase, "/api/article/v1/article/live-legacy", "", 200, 200, "# 旧对象内容")
	}
	var publication model.ArticlePublication
	if err = db.First(&publication, "article_id = ?", "live-modern").Error; err != nil {
		t.Fatal(err)
	}
	var revisions int64
	if err = db.Model(&model.ArticleRevision{}).Where("article_id = ?", "live-modern").Count(&revisions).Error; err != nil {
		t.Fatal(err)
	}
	t.Logf("PG article_publication: state=%s pointer_version=%d current_revision=%s; article_revision_count=%d", publication.State, publication.PointerVersion, publication.CurrentRevision, revisions)
}
