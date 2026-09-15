package main

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

const storageMigrationPath = "../../scripts/migrate-subjectref-v2-storage.sql"

func applyStorageMigration(t *testing.T, conn *pgx.Conn) error {
	t.Helper()
	raw, err := os.ReadFile(storageMigrationPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(context.Background(), string(raw), pgx.QueryExecModeSimpleProtocol)
	if err != nil {
		// The migration deliberately leaves no partial DDL/rows on error. A
		// caller can reuse this isolated connection after rolling back its tx.
		if _, rollbackErr := conn.Exec(context.Background(), "ROLLBACK"); rollbackErr != nil {
			t.Fatalf("migration error %v; rollback error %v", err, rollbackErr)
		}
	}
	return err
}

func storageCount(t *testing.T, conn *pgx.Conn, relation string) int64 {
	t.Helper()
	var count int64
	if err := conn.QueryRow(context.Background(), "SELECT count(*) FROM "+relation).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

type oldAnswerBytes struct {
	AnswerID, SearchID, TurnHash, TurnJSON string
	Ordinal                                int64
}

func frozenAnswer(t *testing.T, conn *pgx.Conn, answerID string) oldAnswerBytes {
	t.Helper()
	var got oldAnswerBytes
	err := conn.QueryRow(context.Background(), `SELECT answer_id,search_id,turn_hash,turn_json,accepted_ordinal
 FROM knowledge_accepted_answers WHERE answer_id=$1`, answerID).Scan(
		&got.AnswerID, &got.SearchID, &got.TurnHash, &got.TurnJSON, &got.Ordinal)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func assertNoStorageSidecars(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	for _, relation := range []string{
		"knowledge_answer_sessions_subject_v2", "knowledge_accepted_answers_subject_v2",
		"knowledge_product_search_operations_subject_v2", "knowledge_tool_parents_subject_v2",
	} {
		var registered *string
		if err := conn.QueryRow(context.Background(), "SELECT to_regclass($1)::text", relation).Scan(&registered); err != nil {
			t.Fatal(err)
		}
		if registered != nil {
			t.Fatalf("failed migration left a sidecar: %s", relation)
		}
	}
}

func TestSubjectRefV2StorageIsolatedPostgres(t *testing.T) {
	conn := fixtureDB(t)
	var version int
	if err := conn.QueryRow(context.Background(), "SELECT current_setting('server_version_num')::integer").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version < 160000 || version >= 170000 {
		t.Fatalf("storage gate needs PostgreSQL 16, got %d", version)
	}
	addSession(t, conn, "rtw.identity", "platform", "42", "same-session", 1)
	addSession(t, conn, "rtw.identity", "platform", "43", "same-session", 1)
	firstTurn := writerTurnJSON(t, "answer-42", "rtw.identity", "platform", "42", "same-session", "search-42", turnVariation{})
	secondTurn := writerTurnJSON(t, "answer-43", "rtw.identity", "platform", "43", "same-session", "search-43", turnVariation{})
	assertInsufficientWriterShape(t, firstTurn, "answer-42", "search-42", "42")
	assertInsufficientWriterShape(t, secondTurn, "answer-43", "search-43", "43")
	addAnswerJSON(t, conn, "answer-42", "rtw.identity", "platform", "42", "same-session", "search-42", 1, firstTurn, false)
	addAnswerJSON(t, conn, "answer-43", "rtw.identity", "platform", "43", "same-session", "search-43", 1, secondTurn, false)
	// Pending product/Tool operations are allowed before an answer Session.
	addOperation(t, conn, "rtw.identity", "platform", "42", "same-session", "product-key", "search-product", "answer-product", "pending")
	addOperation(t, conn, "rtw.identity", "platform", "99", "pending-session", "pending-key", "search-pending", "answer-pending", "pending")
	addToolParent(t, conn, "rtw.identity", "platform", "43", "pending-session", "tool-key", "tool-parent-43")
	_, err := conn.Exec(context.Background(), `INSERT INTO knowledge_outbox(event_id,event_type,aggregate_id,payload)
 VALUES('old-event','knowledge.accepted.v1','answer-42','{"subject":{"authority_id":"rtw.identity","tenant_id":"platform","subject_id":"42"}}')`)
	if err != nil {
		t.Fatal(err)
	}
	var oldEvent string
	if err = conn.QueryRow(context.Background(), "SELECT payload::text FROM knowledge_outbox WHERE event_id='old-event'").Scan(&oldEvent); err != nil {
		t.Fatal(err)
	}
	before42, before43 := frozenAnswer(t, conn, "answer-42"), frozenAnswer(t, conn, "answer-43")
	beforePreflight, err := preflight(context.Background(), conn)
	if err != nil || beforePreflight.Blocking || len(beforePreflight.Findings) != 0 {
		t.Fatalf("old stage-1 preflight must pass: %+v %v", beforePreflight, err)
	}
	if err = applyStorageMigration(t, conn); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	if err = applyStorageMigration(t, conn); err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	for relation, want := range map[string]int64{
		"knowledge_answer_sessions_subject_v2":           2,
		"knowledge_accepted_answers_subject_v2":          2,
		"knowledge_product_search_operations_subject_v2": 2,
		"knowledge_tool_parents_subject_v2":              1,
	} {
		if got := storageCount(t, conn, relation); got != want {
			t.Fatalf("%s projected %d, want %d", relation, got, want)
		}
	}
	var otherAnswer string
	if err := conn.QueryRow(context.Background(), `SELECT a.answer_id FROM knowledge_accepted_answers_subject_v2 p
 JOIN knowledge_accepted_answers a ON
  (a.answer_id,a.authority_id,a.tenant_id,a.subject_id,a.session_id,a.accepted_ordinal)=
  (p.answer_id,p.issuer,p.tenant_id,p.subject_id,p.session_id,p.accepted_ordinal)
 WHERE p.issuer='rtw.identity' AND p.subject_id='43' AND p.session_id='same-session'`).Scan(&otherAnswer); err != nil || otherAnswer != "answer-43" {
		t.Fatalf("same session crossed owners: %q %v", otherAnswer, err)
	}
	if before42 != frozenAnswer(t, conn, "answer-42") || before43 != frozenAnswer(t, conn, "answer-43") {
		t.Fatal("old AnswerID/SearchID/ordinal/turn_hash/turn_json changed")
	}
	var afterEvent string
	if err := conn.QueryRow(context.Background(), "SELECT payload::text FROM knowledge_outbox WHERE event_id='old-event'").Scan(&afterEvent); err != nil || oldEvent != afterEvent {
		t.Fatalf("old outbox payload changed: %v", err)
	}
	afterPreflight, err := preflight(context.Background(), conn)
	if err != nil || !reflect.DeepEqual(beforePreflight, afterPreflight) {
		t.Fatalf("old read-only preflight changed: %+v %+v %v", beforePreflight, afterPreflight, err)
	}

	// CHECK and exact legacy FKs reject identity guesses and mixed-row binds.
	for _, subject := range []string{"0", "01", "+1", "9223372036854775808", "550e8400-e29b-41d4-a716-446655440000"} {
		_, err = conn.Exec(context.Background(), `INSERT INTO knowledge_answer_sessions_subject_v2
  (issuer,tenant_id,subject_id,session_id) VALUES('rtw.identity','platform',$1,'same-session')`, subject)
		if err == nil {
			t.Fatalf("noncanonical UID accepted: %s", subject)
		}
	}
	for _, identity := range [][2]string{{"other.identity", "platform"}, {"rtw.identity", "archive"}} {
		_, err = conn.Exec(context.Background(), `INSERT INTO knowledge_answer_sessions_subject_v2
  (issuer,tenant_id,subject_id,session_id) VALUES($1,$2,'42','same-session')`, identity[0], identity[1])
		if err == nil {
			t.Fatalf("noncanonical issuer/slot accepted: %v", identity)
		}
	}
	_, err = conn.Exec(context.Background(), `INSERT INTO knowledge_answer_sessions_subject_v2
  (issuer,tenant_id,subject_id,session_id) VALUES('rtw.identity','platform','77','absent')`)
	if err == nil {
		t.Fatal("sidecar accepted a session without its old PK")
	}
	_, err = conn.Exec(context.Background(), `INSERT INTO knowledge_accepted_answers_subject_v2
  (answer_id,issuer,tenant_id,subject_id,session_id,accepted_ordinal)
  VALUES('answer-42','rtw.identity','platform','43','same-session',1)`)
	if err == nil {
		t.Fatal("sidecar joined AnswerID 42 to UID 43")
	}
	_, err = conn.Exec(context.Background(), `INSERT INTO knowledge_product_search_operations_subject_v2
  (issuer,tenant_id,subject_id,session_id,operation_key)
  VALUES('rtw.identity','platform','42','same-session','absent')`)
	if err == nil {
		t.Fatal("product sidecar accepted an absent old operation")
	}
	_, err = conn.Exec(context.Background(), `INSERT INTO knowledge_tool_parents_subject_v2
  (issuer,tenant_id,subject_id,session_id,operation_key)
  VALUES('rtw.identity','platform','43','same-session','absent')`)
	if err == nil {
		t.Fatal("Tool sidecar accepted an absent old parent")
	}
	_, err = conn.Exec(context.Background(), "UPDATE knowledge_accepted_answers SET turn_json='changed' WHERE answer_id='answer-42'")
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("old immutable trigger no longer blocks updates: %v", err)
	}

	// An accepted sidecar needs its canonical Session even when the old v1 FK
	// exists. Replaying the operator migration then fills both in one commit.
	addSession(t, conn, "rtw.identity", "platform", "77", "new-session", 1)
	newTurn := writerTurnJSON(t, "answer-77", "rtw.identity", "platform", "77", "new-session", "search-77", turnVariation{})
	addAnswerJSON(t, conn, "answer-77", "rtw.identity", "platform", "77", "new-session", "search-77", 1, newTurn, false)
	_, err = conn.Exec(context.Background(), `INSERT INTO knowledge_accepted_answers_subject_v2
  (answer_id,issuer,tenant_id,subject_id,session_id,accepted_ordinal)
  VALUES('answer-77','rtw.identity','platform','77','new-session',1)`)
	if err == nil {
		t.Fatal("accepted sidecar skipped the v2 Session FK")
	}
	if err = applyStorageMigration(t, conn); err != nil {
		t.Fatalf("replay/new compatible rows: %v", err)
	}
	if storageCount(t, conn, "knowledge_accepted_answers_subject_v2") != 3 {
		t.Fatal("replay did not add the new accepted projection")
	}
}

func TestSubjectRefV2StorageRollsBackHistoricalConflict(t *testing.T) {
	for _, tc := range []struct{ name, issuer, slot, uid string }{
		{"unknown-slot-collision", "rtw.identity", "archive", "42"},
		{"wrong-issuer", "unmapped.identity", "platform", "42"},
		{"bad-uid", "rtw.identity", "platform", "01"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := fixtureDB(t)
			addSession(t, conn, "rtw.identity", "platform", "42", "session", 1)
			turn := writerTurnJSON(t, "answer-good", "rtw.identity", "platform", "42", "session", "search-good", turnVariation{})
			addAnswerJSON(t, conn, "answer-good", "rtw.identity", "platform", "42", "session", "search-good", 1, turn, false)
			before := frozenAnswer(t, conn, "answer-good")
			addSession(t, conn, tc.issuer, tc.slot, tc.uid, "session", 0)
			pre, err := preflight(context.Background(), conn)
			if err != nil || !pre.Blocking {
				t.Fatalf("preflight did not block %s: %+v %v", tc.name, pre, err)
			}
			if err := applyStorageMigration(t, conn); err == nil {
				t.Fatalf("migration projected incompatible history: %s", tc.name)
			}
			assertNoStorageSidecars(t, conn)
			if before != frozenAnswer(t, conn, "answer-good") {
				t.Fatal("failed transaction touched old accepted bytes")
			}
		})
	}
}

func TestSubjectRefV2StorageReplayRejectsCatalogDrift(t *testing.T) {
	for _, tc := range []struct{ name, damage, message string }{
		{"identity-check", `ALTER TABLE knowledge_answer_sessions_subject_v2
 DROP CONSTRAINT knowledge_answer_sessions_subject_v2_identity_ck`, "identity CHECK changed"},
		{"foreign-parent-key", `ALTER TABLE knowledge_accepted_answers_subject_v2
 DROP CONSTRAINT knowledge_accepted_answers_subject_v2_session_fk;
 ALTER TABLE knowledge_accepted_answers_subject_v2
 ADD CONSTRAINT knowledge_accepted_answers_subject_v2_session_fk
 FOREIGN KEY(issuer,tenant_id,subject_id,session_id)
 REFERENCES knowledge_answer_sessions(authority_id,tenant_id,subject_id,session_id) NOT VALID`, "constraint changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := fixtureDB(t)
			if err := applyStorageMigration(t, conn); err != nil {
				t.Fatal(err)
			}
			_, err := conn.Exec(context.Background(), tc.damage, pgx.QueryExecModeSimpleProtocol)
			if err != nil {
				t.Fatal(err)
			}
			if err := applyStorageMigration(t, conn); err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("same-name catalog drift accepted: %v", err)
			}
			if storageCount(t, conn, "knowledge_answer_sessions_subject_v2") != 0 {
				t.Fatal("failed replay inserted projections")
			}
		})
	}
}
