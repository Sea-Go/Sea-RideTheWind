package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// dcJobPlatform is an actual cmd/platform process backed by a fresh database
// in this acceptance script's disposable PostgreSQL cluster. RTW and BTW use
// its public /v1/jobs HTTP contract; the fixed 2D representation remains a
// separate explicit fixture rather than being mistaken for a real DC model.
type dcJobPlatform struct {
	BaseURL string
	Token   string
	Pool    *pgxpool.Pool
}

func startRealDCJobPlatform(t *testing.T, dir, dcRoot string) *dcJobPlatform {
	t.Helper()
	dsn := os.Getenv("KNOWLEDGE_TEST_DSN")
	base, err := url.Parse(dsn)
	if err != nil || base.Scheme != "postgres" || base.Hostname() != "127.0.0.1" {
		t.Fatal("real DC jobs require the task-owned loopback PostgreSQL cluster")
	}
	admin, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	database := "dc_h06_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(context.Background(), "CREATE DATABASE "+pgx.Identifier{database}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	base.Path = "/" + database
	dcDSN := base.String()
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, "DROP DATABASE "+pgx.Identifier{database}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("drop isolated DC jobs database: %v", err)
		}
		admin.Close()
	})
	binary := filepath.Join(dir, "dc-real-jobs-platform")
	compile := exec.Command("go", "build", "-race", "-mod=readonly", "-o", binary, "./cmd/platform")
	compile.Dir = dcRoot
	if output, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("compile real DC job platform: %v\n%s", err, output)
	}
	version, err := exec.Command("git", "-C", dcRoot, "rev-parse", "HEAD").Output()
	if err != nil || len(strings.TrimSpace(string(version))) != 40 {
		t.Fatal("DC platform source SHA unavailable")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	logPath := filepath.Join(dir, "dc-real-jobs-platform.jsonl")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	const token = "test-only-h06-dc-job-token"
	cmd := exec.Command(binary, "-listen", address, "-migrate")
	cmd.Dir = dcRoot
	cmd.Env = append(os.Environ(), "DATABASE_URL="+dcDSN, "PLATFORM_SERVICE_TOKEN="+token,
		"SERVICE_VERSION="+strings.TrimSpace(string(version)))
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		select {
		case err := <-exited:
			if err != nil {
				t.Errorf("DC job platform exited unsuccessfully: %v", err)
			}
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
			t.Error("DC job platform did not stop after SIGTERM")
		}
		logFile.Close()
		logs, _ := os.ReadFile(logPath)
		if !bytes.Contains(logs, []byte(`"event":"platform.started"`)) ||
			!bytes.Contains(logs, []byte(`"event":"platform.stopped"`)) {
			t.Error("real DC job platform omitted structured lifecycle events")
		}
		if t.Failed() {
			t.Logf("DC job platform log: %s", logs)
		}
	})
	client := &http.Client{Timeout: time.Second}
	endpoint := "http://" + address
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		response, err := client.Get(endpoint + "/v1/jobs/not-found")
		if err != nil {
			continue
		}
		response.Body.Close()
		if response.StatusCode == http.StatusUnauthorized {
			break
		}
	}
	response, err := client.Get(endpoint + "/v1/jobs/not-found")
	if err != nil {
		t.Fatalf("DC job platform socket unavailable: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("DC job platform bypassed service authorization: status=%d", response.StatusCode)
	}
	pool, err := pgxpool.New(context.Background(), dcDSN)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return &dcJobPlatform{BaseURL: endpoint, Token: token, Pool: pool}
}

func (p *dcJobPlatform) assertAcceptedIndexJob(t *testing.T, jobID, hash string, dcEpoch int64) {
	t.Helper()
	var state, resultHash string
	var epoch int64
	err := p.Pool.QueryRow(context.Background(), `SELECT state,lease_epoch,result->'result_ref'->>'sha256'
 FROM jobs.job WHERE id=$1`, jobID).Scan(&state, &epoch, &resultHash)
	if err != nil || state != "succeeded" || epoch != dcEpoch || resultHash != hash {
		t.Fatalf("actual DC PostgreSQL job differs from RTW READY: job=%s state=%s epoch=%d hash=%s err=%v",
			jobID, state, epoch, resultHash, err)
	}
	var attempts int
	if err := p.Pool.QueryRow(context.Background(), `SELECT count(*) FROM jobs.attempt
 WHERE job_id=$1 AND lease_epoch=$2 AND result_hash IS NOT NULL`, jobID, dcEpoch).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatalf("actual DC attempt receipt missing or duplicated: count=%d err=%v", attempts, err)
	}
}
