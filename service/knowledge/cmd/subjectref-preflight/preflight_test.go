package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

func TestAmbiguousFrozenTurnKeys(t *testing.T) {
	cases := []struct {
		raw       string
		ambiguous bool
	}{
		{`{"Request":{"AnswerID":"answer"}}`, false},
		{`{"Request":{"AnswerID":"answer","answerid":"answer"}}`, true},
		{`{"Request":{"Subject":{"subject_id":"42","\u0073ubject_id":"42"}}}`, true},
		{`{"Request":{},"\u0052equest":{}}`, true},
		{`{"Request":{"Search":{"Snapshot":{"module_id":"m","\u006dodule_id":"m"}}}}`, true},
		{`{"Request":{"Search":{"Snapshot":{"indexes":{"dense":{},"\u0064ense":{}}}}}}`, true},
		{`{"result":{"search":{"evidence_pack":{"search_id":"search","SEARCH_ID":"search"}}}}`, true},
		{`{"other":1,"other":2}`, false},
	}
	for _, tc := range cases {
		ambiguous, err := ambiguousFrozenTurnKeys([]byte(tc.raw))
		if err != nil || ambiguous != tc.ambiguous {
			t.Errorf("ambiguity=%v want=%v error=%v", ambiguous, tc.ambiguous, err)
		}
	}
	var writer struct{ Request struct{ AnswerID string } }
	if err := json.Unmarshal([]byte(`{"Request":{"AnswerID":"wrong","answerid":"answer"}}`), &writer); err != nil ||
		writer.Request.AnswerID != "answer" {
		t.Fatalf("Go v1 decoder alias behavior changed: %+v %v", writer, err)
	}
	var pack struct {
		SearchID string `json:"search_id"`
	}
	if err := json.Unmarshal([]byte(`{"search_id":"wrong","SEARCH_ID":"search"}`), &pack); err != nil ||
		pack.SearchID != "search" {
		t.Fatalf("Go v1 pack decoder alias behavior changed: %+v %v", pack, err)
	}
	if _, err := ambiguousFrozenTurnKeys([]byte(`{"Request":`)); err == nil {
		t.Fatal("malformed turn key scan succeeded")
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

type turnVariation struct {
	embeddedUID  string
	resultAnswer string
	packSearchID string
}

func writerTurnJSON(t *testing.T, id, issuer, slot, uid, session, search string, variation turnVariation) string {
	t.Helper()
	turnUID := uid
	if variation.embeddedUID != "" {
		turnUID = variation.embeddedUID
	}
	resultAnswer := id
	if variation.resultAnswer != "" {
		resultAnswer = variation.resultAnswer
	}
	packSearch := search
	if variation.packSearchID != "" {
		packSearch = variation.packSearchID
	}
	snapshot := map[string]any{
		"module_id": "module-fixture", "release_id": "release-fixture", "generation": int64(1),
		"publication_revision": "1", "indexes": map[string]any{}, "valid_revision_ids": []string{},
	}
	pack := map[string]any{
		"search_id": packSearch, "snapshot": snapshot, "profile": map[string]any{},
		"status": "empty", "stop_reason": "no_evidence", "coverage_status": "complete",
		"gaps": []string{}, "evidence": []any{},
	}
	turn, err := json.Marshal(map[string]any{
		"Request": map[string]any{
			"SearchID": search, "AnswerID": id, "SessionID": session,
			"Subject": map[string]string{"authority_id": issuer, "tenant_id": slot, "subject_id": turnUID},
			"Search":  map[string]any{"Query": "fixture query", "Depth": "fast", "Intelligence": "low", "Snapshot": snapshot},
		},
		"result": map[string]any{
			"search": map[string]any{"evidence_pack": pack,
				"citation_receipt": map[string]string{"search_id": "", "pack_hash": "", "durable_ref": ""}},
			"answer_id": resultAnswer, "answer": "", "citations": []string{}, "summary_status": "insufficient",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(turn)
}

func addAnswerJSON(t *testing.T, conn *pgx.Conn, id, issuer, slot, uid, session, search string, ordinal int64, raw string, badHash bool) {
	t.Helper()
	h := sha256.Sum256([]byte(raw))
	hash := hex.EncodeToString(h[:])
	if badHash {
		hash = strings.Repeat("0", 64)
	}
	_, err := conn.Exec(context.Background(), `INSERT INTO knowledge_accepted_answers
 (answer_id,authority_id,tenant_id,subject_id,session_id,accepted_ordinal,search_id,status,turn_hash,turn_json)
 VALUES($1,$2,$3,$4,$5,$6,$7,'insufficient',$8,$9)`,
		id, issuer, slot, uid, session, ordinal, search, hash, raw)
	if err != nil {
		t.Fatal(err)
	}
}

func addAnswer(t *testing.T, conn *pgx.Conn, id, issuer, slot, uid, session, search string, ordinal int64, badHash bool, variation turnVariation) {
	t.Helper()
	addAnswerJSON(t, conn, id, issuer, slot, uid, session, search, ordinal,
		writerTurnJSON(t, id, issuer, slot, uid, session, search, variation), badHash)
}

func writerObject(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("writer object missing: %T", value)
	}
	return object
}

func assertInsufficientWriterShape(t *testing.T, raw, id, search, uid string) {
	t.Helper()
	var turn map[string]any
	if err := json.Unmarshal([]byte(raw), &turn); err != nil {
		t.Fatal(err)
	}
	request := writerObject(t, turn["Request"])
	subject := writerObject(t, request["Subject"])
	requestSearch := writerObject(t, request["Search"])
	snapshot := writerObject(t, requestSearch["Snapshot"])
	result := writerObject(t, turn["result"])
	resultSearch := writerObject(t, result["search"])
	pack := writerObject(t, resultSearch["evidence_pack"])
	receipt := writerObject(t, resultSearch["citation_receipt"])
	if request["AnswerID"] != id || request["SearchID"] != search ||
		subject["authority_id"] != "rtw.identity" || subject["tenant_id"] != "platform" || subject["subject_id"] != uid ||
		requestSearch["Query"] == "" || requestSearch["Depth"] != "fast" ||
		requestSearch["Intelligence"] != "low" || snapshot["module_id"] == "" ||
		snapshot["release_id"] == "" || snapshot["generation"] != float64(1) ||
		result["answer_id"] != id || result["summary_status"] != "insufficient" || result["answer"] != "" ||
		pack["search_id"] != search || pack["status"] != "empty" || !reflect.DeepEqual(pack["snapshot"], snapshot) ||
		receipt["search_id"] != "" || receipt["pack_hash"] != "" || receipt["durable_ref"] != "" {
		t.Fatal("fixture lacks the v1 writer's acceptedRootTurn insufficient contract")
	}
	if citations, ok := result["citations"].([]any); !ok || len(citations) != 0 {
		t.Fatal("fixture citations")
	}
	if evidence, ok := pack["evidence"].([]any); !ok || len(evidence) != 0 {
		t.Fatal("fixture evidence")
	}
}

func addAmbiguousAnswer(t *testing.T, conn *pgx.Conn, id, search string, ordinal int64, oldKey, repeatedKeys string) {
	t.Helper()
	const session = "turn-anomaly-session"
	raw := writerTurnJSON(t, id, "rtw.identity", "platform", "42", session, search, turnVariation{})
	if !strings.Contains(raw, oldKey) {
		t.Fatal("fixture replacement key absent")
	}
	raw = strings.Replace(raw, oldKey, repeatedKeys, 1)
	// encoding/json sees the last spelling and can decode a writer-valid turn.
	// The preflight nevertheless blocks the original, ambiguous byte string.
	assertInsufficientWriterShape(t, raw, id, search, "42")
	ambiguous, err := ambiguousFrozenTurnKeys([]byte(raw))
	if err != nil || !ambiguous {
		t.Fatalf("fixture not ambiguous: %v %v", ambiguous, err)
	}
	addAnswerJSON(t, conn, id, "rtw.identity", "platform", "42", session, search, ordinal, raw, false)
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
	goodTurn := writerTurnJSON(t, "answer-good", "rtw.identity", "platform", "42", "session", "search-good", turnVariation{})
	assertInsufficientWriterShape(t, goodTurn, "answer-good", "search-good", "42")
	addAnswerJSON(t, conn, "answer-good", "rtw.identity", "platform", "42", "session", "search-good", 1, goodTurn, false)
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
	addAnswer(t, conn, "answer-bad", "rtw.identity", "other", "42", "session", "search-bad", 1, true, turnVariation{})
	addAnswer(t, conn, "answer-extra", "rtw.identity", "other", "42", "session", "search-extra", 2, false, turnVariation{embeddedUID: "43"})
	addOperation(t, conn, "rtw.identity", "other", "42", "session", "key", "search-bad", "answer-missing", "committed")
	addToolParent(t, conn, "rtw.identity", "other", "42", "session", "key", "tool-bad")
	addSession(t, conn, "rtw.identity", "platform", "042", "another-session", 0)
	addToolParent(t, conn, "dc.account", "platform", "550e8400-e29b-41d4-a716-446655440000", "uuid-session", "key", "tool-uuid")
	addSession(t, conn, "rtw.identity", "platform", "42", "turn-anomaly-session", 5)
	addAnswer(t, conn, "answer-result", "rtw.identity", "platform", "42", "turn-anomaly-session", "search-result", 1,
		false, turnVariation{resultAnswer: "wrong-result"})
	addAnswer(t, conn, "answer-pack", "rtw.identity", "platform", "42", "turn-anomaly-session", "search-pack", 2,
		false, turnVariation{packSearchID: "wrong-pack"})
	addAmbiguousAnswer(t, conn, "answer-unicode", "search-unicode", 3,
		`"subject_id":"42"`, `"subject_id":"43","\u0073ubject_id":"42"`)
	addAmbiguousAnswer(t, conn, "answer-alias", "search-alias", 4,
		`"AnswerID":"answer-alias"`, `"AnswerID":"answer-alias","answerid":"answer-alias"`)
	addAmbiguousAnswer(t, conn, "answer-packdup", "search-packdup", 5,
		`"search_id":"search-packdup"`, `"search_id":"search-packdup","SEARCH_ID":"search-packdup"`)
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
		"knowledge_accepted_answers.turn_result_answer_mismatch",
		"knowledge_accepted_answers.turn_pack_search_mismatch",
		"knowledge_accepted_answers.ambiguous_turn_identity",
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

func TestPreflightRejectsIneffectiveImmutableTriggers(t *testing.T) {
	for _, tc := range []struct{ name, definition string }{
		{"statement", `CREATE TRIGGER knowledge_accepted_answer_immutable BEFORE UPDATE OR DELETE
 ON knowledge_accepted_answers FOR EACH STATEMENT EXECUTE FUNCTION knowledge_immutable_payload()`},
		{"conditional", `CREATE TRIGGER knowledge_accepted_answer_immutable BEFORE UPDATE OR DELETE
 ON knowledge_accepted_answers FOR EACH ROW WHEN (false) EXECUTE FUNCTION knowledge_immutable_payload()`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := fixtureDB(t)
			if _, err := conn.Exec(context.Background(), "DROP TRIGGER knowledge_accepted_answer_immutable ON knowledge_accepted_answers"); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(context.Background(), tc.definition); err != nil {
				t.Fatal(err)
			}
			r, err := preflight(context.Background(), conn)
			if err != nil {
				t.Fatal(err)
			}
			if r.Counts["knowledge_accepted_answers.immutable_trigger_missing"] != 1 {
				t.Fatalf("ineffective trigger passed: %+v", r.Counts)
			}
		})
	}
}

func TestPreflightRejectsMissingResultAndPackIdentity(t *testing.T) {
	conn := fixtureDB(t)
	const session = "missing-fields-session"
	addSession(t, conn, "rtw.identity", "platform", "42", session, 2)
	resultRaw := writerTurnJSON(t, "answer-missing-result", "rtw.identity", "platform", "42", session,
		"search-missing-result", turnVariation{})
	packRaw := writerTurnJSON(t, "answer-missing-pack", "rtw.identity", "platform", "42", session,
		"search-missing-pack", turnVariation{})
	var resultTurn, packTurn map[string]any
	if err := json.Unmarshal([]byte(resultRaw), &resultTurn); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(packRaw), &packTurn); err != nil {
		t.Fatal(err)
	}
	delete(writerObject(t, resultTurn["result"]), "answer_id")
	packResult := writerObject(t, packTurn["result"])
	packSearch := writerObject(t, packResult["search"])
	delete(writerObject(t, packSearch["evidence_pack"]), "search_id")
	resultBytes, err := json.Marshal(resultTurn)
	if err != nil {
		t.Fatal(err)
	}
	packBytes, err := json.Marshal(packTurn)
	if err != nil {
		t.Fatal(err)
	}
	addAnswerJSON(t, conn, "answer-missing-result", "rtw.identity", "platform", "42", session,
		"search-missing-result", 1, string(resultBytes), false)
	addAnswerJSON(t, conn, "answer-missing-pack", "rtw.identity", "platform", "42", session,
		"search-missing-pack", 2, string(packBytes), false)
	r, err := preflight(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Blocking || r.Counts["knowledge_accepted_answers.turn_result_answer_mismatch"] != 1 ||
		r.Counts["knowledge_accepted_answers.turn_pack_search_mismatch"] != 1 ||
		r.Counts["knowledge_accepted_answers.turn_hash_mismatch"] != 0 ||
		r.Counts["knowledge_accepted_answers.turn_identity_mismatch"] != 0 {
		t.Fatalf("missing result or pack identity accepted: %+v", r.Counts)
	}
	var storedResult, storedPack string
	if err := conn.QueryRow(context.Background(), `SELECT turn_json FROM knowledge_accepted_answers WHERE answer_id='answer-missing-result'`).Scan(&storedResult); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(context.Background(), `SELECT turn_json FROM knowledge_accepted_answers WHERE answer_id='answer-missing-pack'`).Scan(&storedPack); err != nil {
		t.Fatal(err)
	}
	if storedResult != string(resultBytes) || storedPack != string(packBytes) {
		t.Fatal("preflight changed frozen turn bytes")
	}
}

func TestPreflightBlocksDeepWriterAcceptableMetadata(t *testing.T) {
	conn := fixtureDB(t)
	const session = "deep-metadata-session"
	addSession(t, conn, "rtw.identity", "platform", "42", session, 1)
	raw := writerTurnJSON(t, "answer-deep", "rtw.identity", "platform", "42", session, "search-deep", turnVariation{})
	metadata := strings.Repeat("[", maxTurnKeyScanDepth+1) + "0" + strings.Repeat("]", maxTurnKeyScanDepth+1)
	raw = strings.TrimSuffix(raw, "}") + `,"metadata":` + metadata + "}"
	if !json.Valid([]byte(raw)) {
		t.Fatal("deep fixture malformed")
	}
	var writer struct {
		Request struct{ AnswerID string }
		Result  struct {
			AnswerID string `json:"answer_id"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(raw), &writer); err != nil || writer.Request.AnswerID != "answer-deep" ||
		writer.Result.AnswerID != "answer-deep" {
		t.Fatalf("deep unknown metadata did not decode as a v1 writer turn: %v", err)
	}
	addAnswerJSON(t, conn, "answer-deep", "rtw.identity", "platform", "42", session, "search-deep", 1, raw, false)
	r, err := preflight(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Blocking || r.Counts["knowledge_accepted_answers.turn_key_scan_failed"] != 1 ||
		r.Counts["knowledge_accepted_answers.turn_hash_mismatch"] != 0 ||
		r.Counts["knowledge_accepted_answers.turn_identity_mismatch"] != 0 {
		t.Fatalf("deep writer-acceptable metadata was silently accepted: %+v", r.Counts)
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
