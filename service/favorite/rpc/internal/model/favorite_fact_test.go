package model

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func favoriteFactStore(t *testing.T) *FavoriteModel {
	t.Helper()
	dsn := os.Getenv("FAVORITE_TEST_DSN")
	if dsn == "" {
		t.Skip("run favorite/rpc/acceptance.sh with isolated PostgreSQL 16")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "favorite_fact_" + uuid.NewString()
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
	if err := db.AutoMigrate(&FavoriteFolder{}, &FavoriteItem{}); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("001_favorite_fact_outbox.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(migration)).Error; err != nil {
		t.Fatal(err)
	}
	return NewFavoriteModel(db)
}

func favoriteOutboxRows(t *testing.T, store *FavoriteModel) []FavoriteFactOutbox {
	t.Helper()
	var rows []FavoriteFactOutbox
	if err := store.conn.Order("favorite_id,aggregate_version").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestFavoriteFactPostgresAssertRetractAndFolderCascade(t *testing.T) {
	store := favoriteFactStore(t)
	ctx := context.Background()
	if err := store.InsertFolder(ctx, &FavoriteFolder{FolderId: 41, UserId: 1001, Name: "main"}); err != nil {
		t.Fatal(err)
	}
	first := &FavoriteItem{FavoriteId: 501, FolderId: 41, UserId: 1001,
		TargetType: "article", TargetId: "article-77", Title: "kept"}
	if err := store.InsertFavorite(ctx, first); err != nil {
		t.Fatal(err)
	}
	rows := favoriteOutboxRows(t, store)
	if len(rows) != 1 || rows[0].EventID != "favorite.501.v1" || rows[0].AggregateVersion != 1 {
		t.Fatalf("favorite assert outbox: %+v", rows)
	}
	var event struct {
		EventType string `json:"event_type"`
		Producer  string `json:"producer"`
		Payload   struct {
			Subject struct {
				AuthorityID string `json:"authority_id"`
				TenantID    string `json:"tenant_id"`
				SubjectID   string `json:"subject_id"`
			} `json:"subject_ref"`
			TargetID       string  `json:"target_id"`
			TargetRevision *string `json:"target_revision"`
			Operation      string  `json:"operation"`
			SourceRef      string  `json:"source_ref"`
		} `json:"payload"`
	}
	if err := json.Unmarshal([]byte(rows[0].Payload), &event); err != nil ||
		event.Producer != "rtw.community.favorite" || event.EventType != "rtw.favorite.assert" ||
		event.Payload.Subject.AuthorityID != "rtw.identity" ||
		event.Payload.Subject.TenantID != "platform" ||
		event.Payload.Subject.SubjectID != "1001" ||
		event.Payload.TargetID != "article-77" || event.Payload.TargetRevision != nil ||
		event.Payload.Operation != "assert" || event.Payload.SourceRef != "rtw.favorite/501" {
		t.Fatalf("favorite H09 envelope %+v err=%v", event, err)
	}
	if err := store.InsertFavorite(ctx, &FavoriteItem{FavoriteId: 502, FolderId: 41, UserId: 1001,
		TargetType: "article", TargetId: "article-77"}); err == nil || len(favoriteOutboxRows(t, store)) != 1 {
		t.Fatal("duplicate folder target created a second fact")
	}
	if err := store.DeleteFavoriteByFavoriteId(ctx, 501, 1002); !errors.Is(err, ErrFavoriteOwnerMismatch) {
		t.Fatalf("other user removed favorite: %v", err)
	}
	if err := store.DeleteFavoriteByFavoriteId(ctx, 501, 1001); err != nil {
		t.Fatal(err)
	}
	rows = favoriteOutboxRows(t, store)
	if len(rows) != 2 || rows[1].EventID != "favorite.501.v2" || rows[1].AggregateVersion != 2 {
		t.Fatalf("favorite retract outbox: %+v", rows)
	}
	if err := store.DeleteFavoriteByFavoriteId(ctx, 501, 1001); !errors.Is(err, ErrorNotFound) ||
		len(favoriteOutboxRows(t, store)) != 2 {
		t.Fatalf("duplicate retract emitted again: %v", err)
	}
	for _, item := range []*FavoriteItem{
		{FavoriteId: 503, FolderId: 41, UserId: 1001, TargetType: "article", TargetId: "article-77"},
		{FavoriteId: 504, FolderId: 41, UserId: 1001, TargetType: "article", TargetId: "article-88"},
	} {
		if err := store.InsertFavorite(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.DeleteFolderCascade(ctx, 41, 1002); !errors.Is(err, ErrFavoriteOwnerMismatch) {
		t.Fatalf("other user deleted folder: %v", err)
	}
	if err := store.DeleteFolderCascade(ctx, 41, 1001); err != nil {
		t.Fatal(err)
	}
	rows = favoriteOutboxRows(t, store)
	if len(rows) != 6 || rows[3].EventID != "favorite.503.v2" || rows[5].EventID != "favorite.504.v2" {
		t.Fatalf("folder cascade lost per-item retract: %+v", rows)
	}
	if err := store.DeleteFolderCascade(ctx, 41, 1001); !errors.Is(err, ErrorNotFound) ||
		len(favoriteOutboxRows(t, store)) != 6 {
		t.Fatalf("folder replay emitted again: %v", err)
	}
}

func TestFavoriteFactPostgresRollbackAndConcurrentDuplicate(t *testing.T) {
	store := favoriteFactStore(t)
	ctx := context.Background()
	if err := store.InsertFolder(ctx, &FavoriteFolder{FolderId: 42, UserId: 1001, Name: "saved"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []int64{601, 602} {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			results <- store.InsertFavorite(ctx, &FavoriteItem{FavoriteId: id, FolderId: 42,
				UserId: 1001, TargetType: "article", TargetId: "one"})
		}(id)
	}
	wg.Wait()
	close(results)
	var succeeded, rejected int
	for err := range results {
		if err == nil {
			succeeded++
		} else {
			rejected++
		}
	}
	if succeeded != 1 || rejected != 1 || len(favoriteOutboxRows(t, store)) != 1 {
		t.Fatalf("concurrent favorite add count: success=%d rejected=%d", succeeded, rejected)
	}
	var item FavoriteItem
	if err := store.conn.Where("folder_id = ?", 42).First(&item).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.conn.Exec(`CREATE FUNCTION deny_retract() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN IF NEW.aggregate_version=2 THEN RAISE EXCEPTION 'injected outbox failure'; END IF;
		RETURN NEW; END $$;
		CREATE TRIGGER deny_retract BEFORE INSERT ON favorite_fact_outbox
		FOR EACH ROW EXECUTE FUNCTION deny_retract()`).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteFavoriteByFavoriteId(ctx, item.FavoriteId, 1001); err == nil {
		t.Fatal("outbox insert failure did not roll back favorite delete")
	}
	if _, err := store.FindFavoriteByFavoriteId(ctx, item.FavoriteId); err != nil ||
		len(favoriteOutboxRows(t, store)) != 1 {
		t.Fatalf("business row and outbox diverged after rollback: %v", err)
	}
}
