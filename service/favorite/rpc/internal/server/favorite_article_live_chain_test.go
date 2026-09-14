package server_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"sea-try-go/service/article/rpc/articleservice"
	"sea-try-go/service/favorite/rpc/internal/model"
	"sea-try-go/service/favorite/rpc/internal/server"
	"sea-try-go/service/favorite/rpc/internal/svc"
	favoritepb "sea-try-go/service/favorite/rpc/pb"

	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

type favoriteArticleFixture struct {
	ArticleAddress string `json:"article_address"`
	ControlURL     string `json:"control_url"`
}

func startFavoriteArticleFixture(t *testing.T, dsn string) favoriteArticleFixture {
	t.Helper()
	root := filepath.Clean("../../../../..")
	readyPath := filepath.Join(t.TempDir(), "article-ready.json")
	releasePath := filepath.Join(filepath.Dir(readyPath), "article-release")
	outputPath := filepath.Join(filepath.Dir(readyPath), "article-fixture.log")
	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	child := exec.Command("go", "test", "-mod=readonly", "-count=1", "-run", "^TestFavoriteArticleFixture$", "./service/article/rpc")
	child.Dir = root
	child.Env = append(os.Environ(), "ARTICLE_FAVORITE_DSN="+dsn, "ARTICLE_FAVORITE_READY_FILE="+readyPath, "ARTICLE_FAVORITE_RELEASE_FILE="+releasePath)
	child.Stdout, child.Stderr = output, output
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- child.Wait() }()
	childDone := false
	childResult := func() (error, string) {
		if !childDone {
			return nil, ""
		}
		if err := output.Close(); err != nil {
			return err, ""
		}
		body, readErr := os.ReadFile(outputPath)
		return readErr, string(body)
	}
	t.Cleanup(func() {
		_ = os.WriteFile(releasePath, []byte("release\n"), 0600)
		if !childDone {
			select {
			case err := <-waited:
				childDone = true
				if err != nil {
					_, body := childResult()
					t.Errorf("Article fixture: %v\n%s", err, body)
				}
			case <-time.After(10 * time.Second):
				_ = child.Process.Kill()
				<-waited
			}
		}
	})
	var fixture favoriteArticleFixture
	deadline := time.Now().Add(30 * time.Second)
	for {
		body, err := os.ReadFile(readyPath)
		if err == nil && json.Unmarshal(body, &fixture) == nil && fixture.ArticleAddress != "" && fixture.ControlURL != "" {
			break
		}
		select {
		case childErr := <-waited:
			childDone = true
			_, childLog := childResult()
			t.Fatalf("Article fixture exited before ready: %v\n%s", childErr, childLog)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("Article fixture not ready: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fixture
}

// TestFavoriteArticleLiveChain starts Article in a separate test process and
// crosses Article network gRPC -> Favorite network gRPC -> isolated PG.
func TestFavoriteArticleLiveChain(t *testing.T) {
	dsn := os.Getenv("FAVORITE_TEST_DSN")
	if dsn == "" {
		t.Skip("set FAVORITE_TEST_DSN with isolated PostgreSQL 16")
	}
	fixture := startFavoriteArticleFixture(t, dsn)
	articleClient, err := zrpc.NewClient(zrpc.NewDirectClientConf([]string{fixture.ArticleAddress}, "", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer articleClient.Conn().Close()
	store, db := favoriteRPCStore(t)
	service := &svc.ServiceContext{FavoriteModel: store, UserRpc: activeUsers{}, ArticleRpc: articleservice.NewArticleService(articleClient)}
	favoriteListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	favoriteServer := grpc.NewServer()
	favoritepb.RegisterFavoriteServiceServer(favoriteServer, server.NewFavoriteServiceServer(service))
	go func() { _ = favoriteServer.Serve(favoriteListener) }()
	t.Cleanup(func() { favoriteServer.Stop(); _ = favoriteListener.Close() })
	conn, err := grpc.NewClient(favoriteListener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := favoritepb.NewFavoriteServiceClient(conn)
	ctx := context.Background()
	folder1, err := client.CreateFavoriteFolder(ctx, &favoritepb.CreateFavoriteFolderReq{UserId: 1001, Name: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	r1, err := client.CreateFavorite(ctx, &favoritepb.CreateFavoriteReq{UserId: 1001, FolderId: folder1.FolderId, TargetType: "article", TargetId: "article-live", Title: "forged r2", Cover: "forged"})
	if err != nil {
		t.Fatal(err)
	}
	assertFavoriteRevision(t, db, r1.FavoriteId, 1, "article-live:r1")
	stored, err := store.FindFavoriteByFavoriteId(ctx, r1.FavoriteId)
	if err != nil || stored.Title != "published r1" || stored.TargetRevision == nil || *stored.TargetRevision != "article-live:r1" {
		t.Fatalf("r1 public projection was not frozen: %+v %v", stored, err)
	}
	for _, target := range []string{"article-draft", "article-withdrawn"} {
		if _, err := client.CreateFavorite(ctx, &favoritepb.CreateFavoriteReq{UserId: 1001, FolderId: folder1.FolderId, TargetType: "article", TargetId: target}); status.Code(err) != codes.NotFound {
			t.Fatalf("%s became favorite: %v", target, err)
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, fixture.ControlURL+"/publish-r2", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("publish r2: HTTP %d", response.StatusCode)
	}
	if _, err := client.CreateFavorite(ctx, &favoritepb.CreateFavoriteReq{UserId: 1001, FolderId: folder1.FolderId, TargetType: "article", TargetId: "article-live"}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("r2 publication rewrote or duplicated existing r1 favorite: %v", err)
	}
	assertFavoriteRevision(t, db, r1.FavoriteId, 1, "article-live:r1")
	var oldEventCount int64
	if err := db.Model(&model.FavoriteFactOutbox{}).Where("favorite_id = ?", r1.FavoriteId).Count(&oldEventCount).Error; err != nil || oldEventCount != 1 {
		t.Fatalf("r2 publication added or changed r1 evidence: count=%d err=%v", oldEventCount, err)
	}
	if _, err := client.DeleteFavorite(ctx, &favoritepb.DeleteFavoriteReq{UserId: 1001, FavoriteId: r1.FavoriteId}); err != nil {
		t.Fatal(err)
	}
	assertFavoriteRevision(t, db, r1.FavoriteId, 2, "article-live:r1")
	folder2, err := client.CreateFavoriteFolder(ctx, &favoritepb.CreateFavoriteFolderReq{UserId: 1001, Name: "r2"})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := client.CreateFavorite(ctx, &favoritepb.CreateFavoriteReq{UserId: 1001, FolderId: folder2.FolderId, TargetType: "article", TargetId: "article-live"})
	if err != nil {
		t.Fatal(err)
	}
	assertFavoriteRevision(t, db, r2.FavoriteId, 1, "article-live:r2")
}

func assertFavoriteRevision(t *testing.T, db *gorm.DB, favoriteID, version int64, want string) {
	t.Helper()
	var row model.FavoriteFactOutbox
	if err := db.Where("favorite_id = ? AND aggregate_version = ?", favoriteID, version).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	var event struct {
		Payload struct {
			TargetRevision *string `json:"target_revision"`
		} `json:"payload"`
	}
	if err := json.Unmarshal([]byte(row.Payload), &event); err != nil || event.Payload.TargetRevision == nil || *event.Payload.TargetRevision != want {
		t.Fatalf("favorite=%d version=%d revision=%+v err=%v payload=%s", favoriteID, version, event.Payload.TargetRevision, err, row.Payload)
	}
}
