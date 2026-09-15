package server_test

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"sea-try-go/service/article/rpc/articleservice"
	articlepb "sea-try-go/service/article/rpc/pb"
	"sea-try-go/service/common/logger"
	favoritecommon "sea-try-go/service/favorite/common"
	"sea-try-go/service/favorite/rpc/internal/model"
	"sea-try-go/service/favorite/rpc/internal/server"
	"sea-try-go/service/favorite/rpc/internal/svc"
	pb "sea-try-go/service/favorite/rpc/pb"
	"sea-try-go/service/user/user/rpc/userservice"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type activeUsers struct{ userservice.UserService }

func (activeUsers) GetUser(_ context.Context, req *userservice.GetUserReq, _ ...grpc.CallOption) (*userservice.GetUserResp, error) {
	active := int64(0)
	return &userservice.GetUserResp{Found: true,
		User: &userservice.UserInfo{Uid: req.Uid, Status: &active}}, nil
}

func TestFavoriteGRPCClassifiesLegacyMissingAssertAsBlocked(t *testing.T) {
	store, db := favoriteRPCStore(t)
	ctx := context.Background()
	if err := store.InsertFolder(ctx, &model.FavoriteFolder{FolderId: 9201, UserId: 1001, Name: "unmapped"}); err != nil {
		t.Fatal(err)
	}
	item := model.FavoriteItem{FavoriteId: 9202, FolderId: 9201, UserId: 1001,
		TargetType: "article", TargetId: "historical-no-assert"}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	service := server.NewFavoriteServiceServer(&svc.ServiceContext{FavoriteModel: store, UserRpc: activeUsers{}})
	if result, err := service.DeleteFavorite(ctx, &pb.DeleteFavoriteReq{UserId: 1001, FavoriteId: 9202}); result != nil || status.Code(err) != codes.FailedPrecondition ||
		favoritecommon.BizCodeFromError(err) != favoritecommon.ErrorFavoriteHistoryBlocked {
		t.Fatalf("legacy missing assert was not a typed blocker: response=%+v err=%v", result, err)
	}
	var itemCount, outboxCount int64
	if err := db.Model(&model.FavoriteItem{}).Where("favorite_id = ?", 9202).Count(&itemCount).Error; err != nil || itemCount != 1 {
		t.Fatalf("RPC blocker deleted old item: count=%d err=%v", itemCount, err)
	}
	if err := db.Model(&model.FavoriteFactOutbox{}).Where("favorite_id = ?", 9202).Count(&outboxCount).Error; err != nil || outboxCount != 0 {
		t.Fatalf("RPC blocker created orphan retract: count=%d err=%v", outboxCount, err)
	}
}

type publishedArticles struct {
	articleservice.ArticleService
	revision *atomic.Int32
}

func (a publishedArticles) GetArticle(_ context.Context, req *articleservice.GetArticleRequest, _ ...grpc.CallOption) (*articleservice.GetArticleResponse, error) {
	if !req.PublicOnly || req.IncrView || req.RequesterId != "" {
		return nil, status.Error(codes.Internal, "favorite bypassed public article projection")
	}
	if req.ArticleId != "article-77" {
		return nil, status.Error(codes.NotFound, "article missing")
	}
	revision, title, cover := "article-77:r1", "Published r1", "r1-cover"
	if a.revision != nil && a.revision.Load() == 2 {
		revision, title, cover = "article-77:r2", "Published r2", "r2-cover"
	}
	return &articleservice.GetArticleResponse{Article: &articleservice.Article{
		Id: "article-77", Status: articlepb.ArticleStatus_PUBLISHED, Title: title,
		CoverImageUrl: cover, ExtInfo: map[string]string{"published_revision_id": revision}}}, nil
}

var favoriteRPCLoggerOnce sync.Once

func favoriteRPCStore(t *testing.T) (*model.FavoriteModel, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("FAVORITE_TEST_DSN")
	if dsn == "" {
		t.Skip("run favorite/rpc/acceptance.sh with isolated PostgreSQL 16")
	}
	favoriteRPCLoggerOnce.Do(func() { logger.Init("favorite-ws02b-rpc-test") })
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "favorite_rpc_" + uuid.NewString()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	address, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := address.Query()
	query.Set("search_path", schema)
	address.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(address.String()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
	})
	if err := db.AutoMigrate(&model.FavoriteFolder{}, &model.FavoriteItem{}); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../model/001_favorite_fact_outbox.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(migration)).Error; err != nil {
		t.Fatal(err)
	}
	deliveryMigration, err := os.ReadFile("../model/002_favorite_fact_delivery.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(deliveryMigration)).Error; err != nil {
		t.Fatal(err)
	}
	revisionMigration, err := os.ReadFile("../model/003_favorite_target_revision.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(revisionMigration)).Error; err != nil {
		t.Fatal(err)
	}
	legacyMigration, err := os.ReadFile("../model/004_favorite_legacy_fact_marker.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(legacyMigration)).Error; err != nil {
		t.Fatal(err)
	}
	return model.NewFavoriteModel(db), db
}

func TestFavoriteGRPCPreservesIDsAndCommitsOutbox(t *testing.T) {
	store, db := favoriteRPCStore(t)
	var sourceRevision atomic.Int32
	service := &svc.ServiceContext{FavoriteModel: store, UserRpc: activeUsers{}, ArticleRpc: publishedArticles{revision: &sourceRevision}}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcServer := grpc.NewServer()
	pb.RegisterFavoriteServiceServer(grpcServer, server.NewFavoriteServiceServer(service))
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() { grpcServer.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := pb.NewFavoriteServiceClient(conn)
	ctx := context.Background()
	folder, err := client.CreateFavoriteFolder(ctx, &pb.CreateFavoriteFolderReq{UserId: 1001, Name: "saved"})
	if err != nil || folder.FolderId <= 0 {
		t.Fatalf("existing folder RPC: %+v %v", folder, err)
	}
	saved, err := client.CreateFavorite(ctx, &pb.CreateFavoriteReq{UserId: 1001, FolderId: folder.FolderId,
		TargetType: "article", TargetId: "article-77", Title: "Private r2", Cover: "r2-cover"})
	if err != nil || saved.FavoriteId <= 0 {
		t.Fatalf("existing favorite RPC: %+v %v", saved, err)
	}
	item, err := store.FindFavoriteByFolderTarget(ctx, folder.FolderId, "article-77", "article")
	if err != nil || item.Title != "Published r1" || item.Cover != "r1-cover" ||
		item.TargetRevision == nil || *item.TargetRevision != "article-77:r1" {
		t.Fatalf("favorite cached client or draft metadata: %+v %v", item, err)
	}
	if _, err := client.CreateFavorite(ctx, &pb.CreateFavoriteReq{UserId: 1001, FolderId: folder.FolderId,
		TargetType: "article", TargetId: "draft", Title: "Private draft"}); status.Code(err) != codes.NotFound {
		t.Fatalf("draft or withdrawn article became a favorite: %v", err)
	}
	if _, err := store.FindFavoriteByFolderTarget(ctx, folder.FolderId, "draft", "article"); err != model.ErrorNotFound {
		t.Fatalf("private article metadata was cached: %v", err)
	}
	var outbox []model.FavoriteFactOutbox
	if err := db.Order("aggregate_version").Find(&outbox).Error; err != nil ||
		len(outbox) != 1 || outbox[0].FavoriteID != saved.FavoriteId {
		t.Fatalf("RPC favorite assert did not commit business outbox: %+v %v", outbox, err)
	}
	var asserted struct {
		Payload struct {
			TargetRevision *string `json:"target_revision"`
		} `json:"payload"`
	}
	if err := json.Unmarshal([]byte(outbox[0].Payload), &asserted); err != nil ||
		asserted.Payload.TargetRevision == nil || *asserted.Payload.TargetRevision != "article-77:r1" {
		t.Fatalf("assert fact did not freeze published r1: %+v %v", asserted, err)
	}
	// The source can publish r2 after the favorite is created. Repeated
	// creation cannot mutate the old favorite or its immutable v1 fact.
	sourceRevision.Store(2)
	if _, err := client.CreateFavorite(ctx, &pb.CreateFavoriteReq{UserId: 1001, FolderId: folder.FolderId,
		TargetType: "article", TargetId: "article-77"}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("duplicate existing RPC returned %v", err)
	}
	if _, err := client.DeleteFavorite(ctx, &pb.DeleteFavoriteReq{UserId: 1002, FavoriteId: saved.FavoriteId}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("other user deleted favorite: %v", err)
	}
	deleted, err := client.DeleteFavorite(ctx, &pb.DeleteFavoriteReq{UserId: 1001, FavoriteId: saved.FavoriteId})
	if err != nil || !deleted.Success {
		t.Fatalf("existing delete RPC: %+v %v", deleted, err)
	}
	outbox = nil
	if err := db.Order("aggregate_version").Find(&outbox).Error; err != nil ||
		len(outbox) != 2 || outbox[1].FavoriteID != saved.FavoriteId ||
		outbox[1].AggregateVersion != 2 {
		t.Fatalf("RPC favorite retract did not retain same object ID: %+v %v", outbox, err)
	}
	var retracted struct {
		Payload struct {
			TargetRevision *string `json:"target_revision"`
		} `json:"payload"`
	}
	if err := json.Unmarshal([]byte(outbox[1].Payload), &retracted); err != nil ||
		retracted.Payload.TargetRevision == nil || *retracted.Payload.TargetRevision != "article-77:r1" {
		t.Fatalf("retract fact followed current r2 instead of frozen r1: %+v %v", retracted, err)
	}
	if _, err := store.FindFavoriteByFavoriteId(ctx, saved.FavoriteId); err != model.ErrorNotFound {
		t.Fatalf("favorite survived retract: %v", err)
	}
}
