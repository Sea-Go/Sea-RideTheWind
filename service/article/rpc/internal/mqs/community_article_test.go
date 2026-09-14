package mqs_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"strconv"
	"sync"
	"testing"

	"sea-try-go/service/article/rpc/internal/logic"
	"sea-try-go/service/article/rpc/internal/model"
	"sea-try-go/service/article/rpc/internal/mqs"
	"sea-try-go/service/article/rpc/internal/svc"
	pb "sea-try-go/service/article/rpc/pb"
	"sea-try-go/service/common/logger"
	"sea-try-go/service/message/rpc/messageservice"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type articleNotifier struct {
	messageservice.MessageService
	calls int
}

var articleLoggerOnce sync.Once

func (n *articleNotifier) SendNotification(context.Context, *messageservice.SendNotificationReq, ...grpc.CallOption) (*messageservice.SendNotificationResp, error) {
	n.calls++
	return &messageservice.SendNotificationResp{}, nil
}

func articleTestRepo(t *testing.T) *model.ArticleRepo {
	t.Helper()
	articleLoggerOnce.Do(func() { logger.Init("article-ws02b-test") })
	dsn := os.Getenv("ARTICLE_TEST_DSN")
	if dsn == "" {
		t.Skip("run article/rpc/acceptance.sh with isolated PostgreSQL 16")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "article_revision_" + uuid.NewString()
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
	if err := db.AutoMigrate(&model.Article{}, &model.ArticleSyncOutboxEvent{}); err != nil {
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

func articlePending(t *testing.T, repo *model.ArticleRepo, id, eventID, markdown, sourcePath, reason string, version int64) {
	t.Helper()
	ctx := context.Background()
	article := &model.Article{ID: id, Title: "Title " + eventID, AuthorID: "1001",
		Status: int32(pb.ArticleStatus_REVIEWING), Content: sourcePath,
		ExtInfo: model.JSONMap{
			mqs.ExtLastSyncEventID: eventID, mqs.ExtLastSyncVersion: strconv.FormatInt(version, 10),
			mqs.ExtLastSyncReason: reason, mqs.ExtPendingSyncReason: reason,
			mqs.ExtRecoSyncState: "pending", mqs.ExtReviewNonce: eventID,
		}}
	if err := repo.Db.WithContext(ctx).Save(article).Error; err != nil {
		t.Fatal(err)
	}
	event := mqs.NewArticleSyncEvent(article, markdown, mqs.ArticleSyncOpUpsert, reason, eventID, version)
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	row := &model.ArticleSyncOutboxEvent{EventID: eventID, EventKey: id + ":upsert:" + eventID,
		EventType: "article_sync", AggregateID: id, Payload: string(body)}
	if err := repo.Db.WithContext(ctx).Create(row).Error; err != nil {
		t.Fatal(err)
	}
}

func acceptArticleResult(t *testing.T, service *svc.ServiceContext, id, eventID string, version int64) error {
	t.Helper()
	body, err := json.Marshal(mqs.ArticleSyncResult{EventScope: mqs.ArticleSyncScope,
		ArticleID: id, EventID: eventID, Op: mqs.ArticleSyncOpUpsert,
		VersionMs: version, Success: true})
	if err != nil {
		t.Fatal(err)
	}
	return mqs.NewArticleSyncResultConsumer(context.Background(), service).
		Consume(context.Background(), id, string(body))
}

func domainRows(t *testing.T, repo *model.ArticleRepo) []model.ArticleDomainOutbox {
	t.Helper()
	var rows []model.ArticleDomainOutbox
	if err := repo.Db.Order("aggregate_version").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestArticlePublishedRevisionsStaleResultAndRetraction(t *testing.T) {
	repo := articleTestRepo(t)
	notifier := &articleNotifier{}
	service := &svc.ServiceContext{ArticleRepo: repo,
		ArticleSyncOutbox: model.NewArticleSyncOutboxModel(repo.Db), MessageRpc: notifier}
	articlePending(t, repo, "article-1", "sync-1", "# First", "article-1.md", mqs.ArticleSyncReasonCreate, 1000)
	if err := acceptArticleResult(t, service, "article-1", "sync-1", 1000); err != nil {
		t.Fatal(err)
	}
	if notifier.calls != 1 {
		t.Fatalf("first publication notification count=%d", notifier.calls)
	}
	first, err := repo.RevisionByID(context.Background(), "article-1", "article-1:r1")
	if err != nil || first.Markdown != "# First" || first.SourceObject != "article-1.md" {
		t.Fatalf("immutable published revision %+v %v", first, err)
	}
	sum := sha256.Sum256([]byte("# First"))
	if first.ContentSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("first revision content hash differs from approved sync outbox")
	}
	pointer, err := repo.Publication(context.Background(), "article-1")
	if err != nil || pointer.PointerVersion != 1 || pointer.CurrentRevision != first.RevisionID ||
		pointer.State != "published" || len(domainRows(t, repo)) != 1 {
		t.Fatalf("first community publication %+v %v", pointer, err)
	}
	if err := acceptArticleResult(t, service, "article-1", "sync-1", 1000); err != nil ||
		len(domainRows(t, repo)) != 1 || notifier.calls != 1 {
		t.Fatalf("duplicate result generated revision or notification: %v", err)
	}
	var envelope struct {
		EventType string `json:"event_type"`
		Payload   struct {
			RevisionID     string  `json:"revision_id"`
			Markdown       string  `json:"markdown"`
			SearchEvidence bool    `json:"search_evidence"`
			WikiModule     *string `json:"wiki_module_id"`
		} `json:"payload"`
	}
	if err := json.Unmarshal([]byte(domainRows(t, repo)[0].Payload), &envelope); err != nil ||
		envelope.EventType != "community.article.published" ||
		envelope.Payload.RevisionID != first.RevisionID || envelope.Payload.Markdown != "# First" ||
		!envelope.Payload.SearchEvidence || envelope.Payload.WikiModule != nil {
		t.Fatalf("H03 community envelope %+v %v", envelope, err)
	}

	articlePending(t, repo, "article-1", "sync-2", "# Second", "article-1-second.md", mqs.ArticleSyncReasonUpdate, 2000)
	staleReview, _ := json.Marshal(mqs.ArticleReviewMessage{ArticleID: "article-1", AuthorID: "1001",
		ContentPath: "article-1.md", ReviewNonce: "sync-1"})
	if err := mqs.NewArticleConsumer(context.Background(), service).Consume(context.Background(),
		"article-1", string(staleReview)); err != nil {
		t.Fatalf("old audit target was not ignored before object IO: %v", err)
	}
	staleTitleOnlyReview, _ := json.Marshal(mqs.ArticleReviewMessage{ArticleID: "article-1", AuthorID: "1001",
		ContentPath: "article-1-second.md", ReviewNonce: "sync-1"})
	if err := mqs.NewArticleConsumer(context.Background(), service).Consume(context.Background(),
		"article-1", string(staleTitleOnlyReview)); err != nil {
		t.Fatalf("old audit nonce approved a later metadata edit with the same object path: %v", err)
	}
	var syncCount int64
	if err := repo.Db.Model(&model.ArticleSyncOutboxEvent{}).Count(&syncCount).Error; err != nil || syncCount != 2 {
		t.Fatalf("stale review created another sync event: count=%d err=%v", syncCount, err)
	}
	if err := acceptArticleResult(t, service, "article-1", "sync-1", 1000); err != nil ||
		len(domainRows(t, repo)) != 1 {
		t.Fatalf("old sync result overwrote newer review: %v", err)
	}
	if err := acceptArticleResult(t, service, "article-1", "sync-2", 2000); err != nil {
		t.Fatal(err)
	}
	second, err := repo.RevisionByID(context.Background(), "article-1", "article-1:r2")
	if err != nil || second.Markdown != "# Second" || second.SourceObject != "article-1-second.md" ||
		second.ContentSHA256 == first.ContentSHA256 {
		t.Fatalf("new review did not bind new bytes: %+v %v", second, err)
	}
	byOldArticleID, err := repo.FindOne(context.Background(), "article-1")
	if err != nil || byOldArticleID.ID != "article-1" || byOldArticleID.Content != second.SourceObject {
		t.Fatalf("legacy article ID did not resolve to current source: %+v %v", byOldArticleID, err)
	}
	if old, err := repo.RevisionByID(context.Background(), "article-1", first.RevisionID); err != nil ||
		old.Markdown != "# First" {
		t.Fatalf("old public article bytes changed: %+v %v", old, err)
	}
	pointer, err = repo.Publication(context.Background(), "article-1")
	if err != nil || pointer.PointerVersion != 2 || pointer.CurrentRevision != second.RevisionID ||
		len(domainRows(t, repo)) != 2 {
		t.Fatalf("second publication %+v %v", pointer, err)
	}
	if err := repo.Db.Model(&model.ArticleRevision{}).
		Where("revision_id = ?", first.RevisionID).Update("markdown", "tampered").Error; err == nil {
		t.Fatal("PG allowed immutable article revision mutation")
	}

	draft := pb.ArticleStatus_DRAFT
	if _, err := logic.NewUpdateArticleLogic(context.Background(), service).
		UpdateArticle(&pb.UpdateArticleRequest{ArticleId: "article-1", Status: &draft}); err != nil {
		t.Fatal(err)
	}
	pointer, err = repo.Publication(context.Background(), "article-1")
	if err != nil || pointer.PointerVersion != 3 || pointer.State != "retracted" ||
		pointer.CurrentRevision != second.RevisionID || len(domainRows(t, repo)) != 3 {
		t.Fatalf("status retract did not atomically fence old content: %+v %v", pointer, err)
	}
	if err := acceptArticleResult(t, service, "article-1", "sync-2", 2000); err != nil {
		t.Fatal(err)
	}
	current, err := repo.FindOne(context.Background(), "article-1")
	if err != nil || current.Status != int32(draft) || len(domainRows(t, repo)) != 3 {
		t.Fatalf("old result restored retracted article: %+v %v", current, err)
	}
}

func TestArticleDomainOutboxFailureRollsBackPublication(t *testing.T) {
	repo := articleTestRepo(t)
	service := &svc.ServiceContext{ArticleRepo: repo,
		ArticleSyncOutbox: model.NewArticleSyncOutboxModel(repo.Db)}
	articlePending(t, repo, "article-rollback", "sync-rollback", "# Fixed",
		"article-rollback.md", mqs.ArticleSyncReasonUpdate, 1000)
	if err := repo.Db.Exec(`CREATE FUNCTION deny_domain_event() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'injected domain outbox failure'; END $$;
		CREATE TRIGGER deny_domain_event BEFORE INSERT ON article_domain_outbox
		FOR EACH ROW EXECUTE FUNCTION deny_domain_event()`).Error; err != nil {
		t.Fatal(err)
	}
	if err := acceptArticleResult(t, service, "article-rollback", "sync-rollback", 1000); err == nil {
		t.Fatal("domain outbox failure was acknowledged as published")
	}
	var revisions int64
	if err := repo.Db.Model(&model.ArticleRevision{}).Count(&revisions).Error; err != nil || revisions != 0 {
		t.Fatalf("revision survived failed transaction: %d %v", revisions, err)
	}
	var pointers int64
	if err := repo.Db.Model(&model.ArticlePublication{}).Count(&pointers).Error; err != nil || pointers != 0 {
		t.Fatalf("pointer survived failed transaction: %d %v", pointers, err)
	}
	article, err := repo.FindOne(context.Background(), "article-rollback")
	if err != nil || article.Status != int32(pb.ArticleStatus_REVIEWING) {
		t.Fatalf("business status changed without outbox: %+v %v", article, err)
	}
}

func TestArticleDeleteRetainsHistoricalRevisionAndOutbox(t *testing.T) {
	repo := articleTestRepo(t)
	service := &svc.ServiceContext{ArticleRepo: repo,
		ArticleSyncOutbox: model.NewArticleSyncOutboxModel(repo.Db), MessageRpc: &articleNotifier{}}
	articlePending(t, repo, "article-delete", "sync-publish", "# Published",
		"article-delete.md", mqs.ArticleSyncReasonCreate, 1000)
	if err := acceptArticleResult(t, service, "article-delete", "sync-publish", 1000); err != nil {
		t.Fatal(err)
	}
	if _, err := logic.NewDeleteArticleLogic(context.Background(), service).
		DeleteArticle(&pb.DeleteArticleRequest{ArticleId: "article-delete", OperatorId: "2002"}); err == nil {
		t.Fatal("other user deleted published article")
	}
	if _, err := logic.NewDeleteArticleLogic(context.Background(), service).
		DeleteArticle(&pb.DeleteArticleRequest{ArticleId: "article-delete", OperatorId: "1001"}); err != nil {
		t.Fatal(err)
	}
	pointer, err := repo.Publication(context.Background(), "article-delete")
	if err != nil || pointer.State != "retracted" || pointer.PointerVersion != 2 ||
		len(domainRows(t, repo)) != 2 {
		t.Fatalf("delete and H03 retract diverged: %+v %v", pointer, err)
	}
	old, err := repo.RevisionByID(context.Background(), "article-delete", "article-delete:r1")
	if err != nil || old.Markdown != "# Published" || old.SourceObject != "article-delete.md" {
		t.Fatalf("historical published body lost after deletion: %+v %v", old, err)
	}
	if _, err := repo.FindOne(context.Background(), "article-delete"); err == nil {
		t.Fatal("old article route still returned logically deleted article")
	}
	if _, err := logic.NewDeleteArticleLogic(context.Background(), service).
		DeleteArticle(&pb.DeleteArticleRequest{ArticleId: "article-delete", OperatorId: "1001"}); err == nil ||
		len(domainRows(t, repo)) != 2 {
		t.Fatalf("duplicate delete emitted an additional retraction: %v", err)
	}
}
