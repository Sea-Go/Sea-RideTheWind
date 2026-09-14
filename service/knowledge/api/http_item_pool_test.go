package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runRealBTWItemPool hands this test's actual RTW publication to a separately
// compiled BTW consumer. Its PostgreSQL pool tables live in a new schema of
// the same isolated server, not in the knowledge service schema.
func runRealBTWItemPool(t *testing.T, btwRoot, rtwBase, workerToken, pgDSN,
	moduleID, revisionID, itemID string) {
	t.Helper()
	dir := t.TempDir()
	fixturePath := filepath.Join(dir, "item-pool-fixture.json")
	fixture, err := json.Marshal(map[string]string{
		"rtw_base": rtwBase, "worker_token": workerToken, "pg_dsn": pgDSN,
		"module_id": moduleID, "revision_id": revisionID, "item_id": itemID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixturePath, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "btw-item-pool.test")
	compileCtx, cancelCompile := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancelCompile()
	compile := exec.CommandContext(compileCtx, "go", "test", "-c", "-mod=readonly", "-race",
		"-o", binary, "./internal/app")
	compile.Dir = btwRoot
	if output, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("compile real BTW item pool consumer: %v\n%s", err,
			redactItemOutput(output, workerToken, pgDSN))
	}
	runCtx, cancelRun := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancelRun()
	child := exec.CommandContext(runCtx, binary, "-test.run=^TestRTWRealItemPoolHandoff$", "-test.v")
	child.Dir = btwRoot
	child.Env = append(os.Environ(), "SEA_RTW_ITEM_POOL_FIXTURE="+fixturePath)
	output, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("RTW published item was not accepted by BTW pool: %v\n%s", err,
			redactItemOutput(output, workerToken, pgDSN))
	}
	if !strings.Contains(string(output), "--- PASS: TestRTWRealItemPoolHandoff") {
		t.Fatalf("BTW item consumer did not report its real handoff test: %s",
			redactItemOutput(output, workerToken, pgDSN))
	}
	t.Logf("RTW current publication %s/%s reached BTW immutable item pool", moduleID, revisionID)
}

func redactItemOutput(output []byte, workerToken, pgDSN string) string {
	message := string(output)
	for _, secret := range []string{workerToken, pgDSN} {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	return message
}
