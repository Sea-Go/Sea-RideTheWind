package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestCanonicalUID(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"1", true}, {"9223372036854775807", true}, {"0", false}, {"01", false},
		{"+1", false}, {"-1", false}, {"9223372036854775808", false},
		{"550e8400-e29b-41d4-a716-446655440000", false}, {" 1", false},
	} {
		if canonicalUID(tc.value) != tc.valid {
			t.Errorf("canonicalUID(%q)=%v want=%v", tc.value, !tc.valid, tc.valid)
		}
	}
}

func fixtureDB(t *testing.T) *pgx.Conn {
	t.Helper()
	dsn := os.Getenv("KNOWLEDGE_TEST_DSN")
	if dsn == "" {
		t.Skip("set KNOWLEDGE_TEST_DSN to a dedicated local PostgreSQL instance")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "preflight_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close(ctx)
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatal(err)
	}
	ddl, err := os.ReadFile("../../api/internal/model/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, string(ddl), pgx.QueryExecModeSimpleProtocol); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin.Exec(context.Background(), "SET search_path TO public")
		admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close(context.Background())
	})
	return admin
}

func addSession(t *testing.T, conn *pgx.Conn, issuer, slot, uid, session string, last int64) {
	t.Helper()
	_, err := conn.Exec(context.Background(), `INSERT INTO knowledge_answer_sessions
 (authority_id,tenant_id,subject_id,session_id,last_ordinal) VALUES($1,$2,$3,$4,$5)`,
		issuer, slot, uid, session, last)
	if err != nil {
		t.Fatal(err)
	}
}

func addAnswer(t *testing.T, conn *pgx.Conn, id, issuer, slot, uid, session, search string, ordinal int64, badHash bool, embeddedUID ...string) {
	t.Helper()
	turnUID := uid
	if len(embeddedUID) > 0 {
		turnUID = embeddedUID[0]
	}
	turn, err := json.Marshal(map[string]any{"Request": map[string]any{
		"AnswerID": id, "SearchID": search, "SessionID": session,
		"Subject": map[string]string{"authority_id": issuer, "tenant_id": slot, "subject_id": turnUID},
	}})
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(turn)
	hash := hex.EncodeToString(h[:])
	if badHash {
		hash = strings.Repeat("0", 64)
	}
	_, err = conn.Exec(context.Background(), `INSERT INTO knowledge_accepted_answers
 (answer_id,authority_id,tenant_id,subject_id,session_id,accepted_ordinal,search_id,status,turn_hash,turn_json)
 VALUES($1,$2,$3,$4,$5,$6,$7,'insufficient',$8,$9)`,
		id, issuer, slot, uid, session, ordinal, search, hash, string(turn))
	if err != nil {
		t.Fatal(err)
	}
}

func addOperation(t *testing.T, conn *pgx.Conn, issuer, slot, uid, session, key, search, answer, status string) {
	t.Helper()
	_, err := conn.Exec(context.Background(), `INSERT INTO knowledge_product_search_operations
 (authority_id,tenant_id,subject_id,session_id,operation_key,request_hash,request_json,snapshot,search_id,answer_id,status)
 VALUES($1,$2,$3,$4,$5,'fixture-hash','{}','{}',$6,$7,$8)`,
		issuer, slot, uid, session, key, search, answer, status)
	if err != nil {
		t.Fatal(err)
	}
}

func addToolParent(t *testing.T, conn *pgx.Conn, issuer, slot, uid, session, key, operation string) {
	t.Helper()
	_, err := conn.Exec(context.Background(), `INSERT INTO knowledge_tool_parents
 (authority_id,tenant_id,subject_id,session_id,operation_key,operation_id,module_id,snapshot,
 snapshot_ref,scope_ref,budget_ref,expires_at,search_remaining,read_remaining,quote_remaining)
 VALUES($1,$2,$3,$4,$5,$6,'module','{}','snapshot','scope',$7,now()+interval '1 hour',1,1,1)`,
		issuer, slot, uid, session, key, operation, "budget-"+operation)
	if err != nil {
		t.Fatal(err)
	}
}

func reportBytes(t *testing.T, r report) []byte {
	t.Helper()
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

func TestPreflightIsolatedPostgres(t *testing.T) {
	conn := fixtureDB(t)
	addSession(t, conn, "rtw.identity", "platform", "42", "session", 1)
	addAnswer(t, conn, "answer-good", "rtw.identity", "platform", "42", "session", "search-good", 1, false)
	addOperation(t, conn, "rtw.identity", "platform", "42", "session", "key", "search-good", "answer-good", "committed")
	addToolParent(t, conn, "rtw.identity", "platform", "42", "session", "key", "tool-good")
	clean, err := preflight(context.Background(), conn)
	if err != nil || clean.Blocking || len(clean.Findings) != 0 {
		t.Fatalf("clean preflight: %+v, %v", clean, err)
	}
	for _, spec := range tables {
		if clean.Rows[spec.name] != 1 {
			t.Fatalf("%s rows=%d", spec.name, clean.Rows[spec.name])
		}
	}
	// V1 permits distinct compatibility slots. V2 projection would merge these
	// otherwise distinct business keys, so the scan must block before migration.
	addSession(t, conn, "rtw.identity", "other", "42", "session", 1)
	addAnswer(t, conn, "answer-bad", "rtw.identity", "other", "42", "session", "search-bad", 1, true)
	addAnswer(t, conn, "answer-extra", "rtw.identity", "other", "42", "session", "search-extra", 2, false, "43")
	addOperation(t, conn, "rtw.identity", "other", "42", "session", "key", "search-bad", "answer-missing", "committed")
	addToolParent(t, conn, "rtw.identity", "other", "42", "session", "key", "tool-bad")
	addSession(t, conn, "rtw.identity", "platform", "042", "another-session", 0)
	addToolParent(t, conn, "dc.account", "platform", "550e8400-e29b-41d4-a716-446655440000", "uuid-session", "key", "tool-uuid")
	bad, err := preflight(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	if !bad.Blocking {
		t.Fatal("anomalies did not block")
	}
	want := []string{
		"knowledge_answer_sessions.invalid_compatibility_slot",
		"knowledge_answer_sessions.invalid_rtw_uid",
		"knowledge_answer_sessions.projected_key_collision",
		"knowledge_accepted_answers.projected_key_collision",
		"knowledge_accepted_answers.session_fk_or_ordinal_mismatch",
		"knowledge_accepted_answers.turn_hash_mismatch",
		"knowledge_accepted_answers.turn_identity_mismatch",
		"knowledge_product_search_operations.committed_answer_link_mismatch",
		"knowledge_product_search_operations.projected_key_collision",
		"knowledge_tool_parents.invalid_issuer",
		"knowledge_tool_parents.invalid_rtw_uid",
		"knowledge_tool_parents.projected_key_collision",
	}
	for _, key := range want {
		if bad.Counts[key] == 0 {
			t.Errorf("missing anomaly %s: %+v", key, bad.Counts)
		}
	}
	second, err := preflight(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	if string(reportBytes(t, bad)) != string(reportBytes(t, second)) {
		t.Fatal("same snapshot produced different report bytes")
	}
	if path := os.Getenv("KNOWLEDGE_PREFLIGHT_TEST_REPORT"); path != "" {
		if err := os.WriteFile(path, reportBytes(t, bad), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range bad.Findings {
		if len(f.KeySHA) != 64 {
			t.Fatal(f)
		}
	}
	if strings.Contains(string(reportBytes(t, bad)), "550e8400") ||
		strings.Contains(string(reportBytes(t, bad)), "answer-bad") ||
		strings.Contains(string(reportBytes(t, bad)), "\"subject_id\": \"42\"") {
		t.Fatal("report exposed source identifiers")
	}
}

func TestPreflightDetectsMissingImmutableTrigger(t *testing.T) {
	conn := fixtureDB(t)
	if _, err := conn.Exec(context.Background(), "DROP TRIGGER knowledge_accepted_answer_immutable ON knowledge_accepted_answers"); err != nil {
		t.Fatal(err)
	}
	r, err := preflight(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	if r.Counts["knowledge_accepted_answers.immutable_trigger_missing"] != 1 {
		t.Fatal(fmt.Sprint(r.Counts))
	}
}

func TestPreflightDetectsMissingLegacyFK(t *testing.T) {
	conn := fixtureDB(t)
	var name string
	if err := conn.QueryRow(context.Background(), `SELECT conname FROM pg_catalog.pg_constraint
 WHERE conrelid='knowledge_accepted_answers'::regclass AND contype='f'
 AND confrelid='knowledge_answer_sessions'::regclass`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(context.Background(), "ALTER TABLE knowledge_accepted_answers DROP CONSTRAINT "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	r, err := preflight(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	if r.Counts["knowledge_accepted_answers.legacy_constraint_missing"] != 1 {
		t.Fatal(fmt.Sprint(r.Counts))
	}
}

func TestFindingCapKeepsBlockingCount(t *testing.T) {
	r := newReport()
	for i := 0; i < maxReportedFindings+1; i++ {
		r.add("knowledge_answer_sessions", "invalid_rtw_uid", []string{fmt.Sprint(i)}, 0)
	}
	if !r.Blocking || len(r.Findings) != maxReportedFindings || r.OmittedFindings != 1 ||
		r.Counts["knowledge_answer_sessions.invalid_rtw_uid"] != maxReportedFindings+1 {
		t.Fatalf("capped report lost anomaly counts: %+v", r)
	}
}

func TestReportFileIsPrivateAndNeverOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	if err := writeReport(path, []byte("first\n")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("report mode: %v %v", info, err)
	}
	if err := writeReport(path, []byte("second\n")); err == nil {
		t.Fatal("existing report overwritten")
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "first\n" {
		t.Fatalf("existing report changed: %q %v", b, err)
	}
}
