package logic

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/user/user/rpc/internal/model"
	"sea-try-go/service/user/user/rpc/internal/svc"
	"sea-try-go/service/user/user/rpc/pb"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestGetUserReportsPresentAccountStatusFromPostgres(t *testing.T) {
	dsn := os.Getenv("KNOWLEDGE_TEST_DSN")
	if dsn == "" {
		t.Skip("set KNOWLEDGE_TEST_DSN to the isolated acceptance PostgreSQL")
	}
	logger.Init("user-rpc-status-test")
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := "user_status_test_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	if _, err := admin.Exec(ctx, "CREATE SCHEMA \""+schema+"\""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA \""+schema+"\" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	underlying, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = underlying.Close() })
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	const uid int64 = 9123
	if err := db.Create(&model.User{Uid: uid, Username: "status-test", Email: "status@example.test", Status: 0}).Error; err != nil {
		t.Fatal(err)
	}
	logic := NewGetUserLogic(ctx, &svc.ServiceContext{UserModel: model.NewUserModel(db)})
	for _, status := range []int64{0, 1, 2} {
		if err := db.Model(&model.User{}).Where("uid = ?", uid).Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
		response, err := logic.GetUser(&pb.GetUserReq{Uid: uid})
		if err != nil || response == nil || !response.Found || response.User == nil ||
			response.User.Status == nil || response.User.GetStatus() != status {
			t.Fatalf("database status=%d rpc response=%+v err=%v", status, response, err)
		}
	}
}
