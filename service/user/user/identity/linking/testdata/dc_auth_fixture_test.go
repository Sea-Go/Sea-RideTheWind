// This file is compiled only through go test -overlay into DataCenter's
// cmd/server package. It exercises the unchanged DC api/nativeauth/UserStore
// implementation while the RTW H01 acceptance suite calls the live endpoint.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"cloud-personal-data-center/internal/api"
	"cloud-personal-data-center/internal/auth"
	"cloud-personal-data-center/internal/config"
	"cloud-personal-data-center/internal/nativeauth"
	"cloud-personal-data-center/internal/store"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

func TestH01RealNativeAuthFixture(t *testing.T) {
	dsn, infoPath, stopPath := os.Getenv("KNOWLEDGE_TEST_DSN"),
		os.Getenv("H01_DC_FIXTURE_INFO"), os.Getenv("H01_DC_FIXTURE_STOP")
	if dsn == "" || infoPath == "" || stopPath == "" {
		t.Fatal("H01 overlay fixture requires isolated PG and control file paths")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	database := "h01_dc_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{database}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	pc, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pc.ConnConfig.Database = database
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{database}.Sanitize()+" WITH (FORCE)")
	}()
	// Only the real native auth tables are needed for this isolated endpoint
	// probe; this does not claim the whole DC migration set passed on PG16.
	_, err = pool.Exec(ctx, `CREATE SCHEMA core;
		CREATE TABLE core.app_user (
			id uuid PRIMARY KEY, email varchar(320) NOT NULL UNIQUE,
			password_hash text NOT NULL, auth_epoch bigint NOT NULL DEFAULT 0,
			nickname varchar(100) NOT NULL, role varchar(20) NOT NULL,
			status varchar(20) NOT NULL, last_login_at timestamptz,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(), deleted_at timestamptz
		)`)
	if err != nil {
		t.Fatal(err)
	}
	users := store.NewUserStore(pool)
	password := "h01-fixture-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := users.Create(ctx, "h01-owner@example.test", string(hash), "Owner", "user", "active")
	if err != nil {
		t.Fatal(err)
	}
	other, err := users.Create(ctx, "h01-other@example.test", string(hash), "Other", "user", "active")
	if err != nil {
		t.Fatal(err)
	}
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	defer redisClient.Close()
	native := nativeauth.New(redisClient, 15*time.Minute, time.Hour,
		nativeauth.WithAuthEpochVerifier(users))
	cfg := config.Config{JWTSecret: []byte(strings.Repeat("j", 32))}
	handler := api.New(users, nil, auth.NewService(cfg), nil, nil, nil, native,
		nil, nil, nil, cfg).Router()
	server := httptest.NewServer(handler)
	defer server.Close()
	issue := func(email string, expected uuid.UUID) string {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"email": email, "password": password})
		resp, err := http.Post(server.URL+"/v1/auth/sessions", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var session struct {
			AccessToken string `json:"accessToken"`
			User        struct {
				ID string `json:"id"`
			} `json:"user"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&session); err != nil ||
			resp.StatusCode != http.StatusCreated ||
			!strings.HasPrefix(session.AccessToken, "wh_access_") ||
			session.User.ID != expected.String() {
			t.Fatalf("native DC login status=%d valid_token=%t expected_user=%t err=%v",
				resp.StatusCode, strings.HasPrefix(session.AccessToken, "wh_access_"),
				session.User.ID == expected.String(), err)
		}
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+session.AccessToken)
		me, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer me.Body.Close()
		var current struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(me.Body).Decode(&current); err != nil ||
			me.StatusCode != http.StatusOK || current.ID != expected.String() {
			t.Fatalf("native DC auth/me status=%d correct_user=%t err=%v",
				me.StatusCode, current.ID == expected.String(), err)
		}
		return session.AccessToken
	}
	ownerBearer := issue("h01-owner@example.test", owner.ID)
	otherBearer := issue("h01-other@example.test", other.ID)
	info, err := json.Marshal(struct {
		URL         string `json:"url"`
		OwnerBearer string `json:"owner_bearer"`
		OtherBearer string `json:"other_bearer"`
	}{server.URL, ownerBearer, otherBearer})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(infoPath, info, 0600); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.WriteFile(infoPath, []byte(`{"stopped":true}`), 0600) }()
	deadline := time.Now().Add(3 * time.Minute)
	for {
		if _, err := os.Stat(stopPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("RTW did not finish the H01 real DC fixture before timeout")
		}
		time.Sleep(100 * time.Millisecond)
	}
	meStatus := func(bearer string) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		return response.StatusCode
	}
	if got := meStatus(otherBearer); got != http.StatusUnauthorized {
		t.Fatalf("DC native DELETE failed to revoke the other session: status=%d", got)
	}
	if _, err := pool.Exec(ctx, `UPDATE core.app_user SET status='disabled',auth_epoch=auth_epoch+1
		WHERE id=$1`, owner.ID); err != nil {
		t.Fatal(err)
	}
	if got := meStatus(ownerBearer); got != http.StatusUnauthorized {
		t.Fatalf("DC native auth epoch failed to disable the owner: status=%d", got)
	}
	// No token or account identifier is written to Go test output.
	t.Log("real DC native auth route served RTW H01; owner and other sessions were independently issued")
}
