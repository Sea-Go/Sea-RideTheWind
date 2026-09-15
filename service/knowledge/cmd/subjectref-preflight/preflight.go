package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"
)

const maxReportedFindings = 200

// Findings contain only deterministic hashes of database keys, never a UID,
// connection string, or accepted turn body. Counts include omitted findings.
type report struct {
	Contract        string           `json:"contract"`
	Rows            map[string]int64 `json:"rows"`
	Counts          map[string]int64 `json:"counts"`
	Findings        []finding        `json:"findings"`
	OmittedFindings int64            `json:"omitted_findings"`
	Blocking        bool             `json:"blocking"`
}

type finding struct {
	Table  string `json:"table"`
	Code   string `json:"code"`
	KeySHA string `json:"key_sha256"`
	Rows   int64  `json:"rows,omitempty"`
}

type tableSpec struct {
	name string
	key  string
	row  string
}

type constraintSpec struct {
	kind    string
	columns []string
	parent  string
}

var tables = []tableSpec{
	{"knowledge_answer_sessions", "session_id", "session_id"},
	{"knowledge_accepted_answers", "session_id,accepted_ordinal", "answer_id"},
	{"knowledge_product_search_operations", "session_id,operation_key", "search_id"},
	{"knowledge_tool_parents", "session_id,operation_key", "operation_id"},
}

func newReport() report {
	return report{Contract: "rtw.knowledge.subjectref-v2.preflight.v1", Rows: map[string]int64{},
		Counts: map[string]int64{}, Findings: []finding{}}
}

func (r *report) add(table, code string, parts []string, rows int64) {
	r.Blocking = true
	r.Counts[table+"."+code]++
	if len(r.Findings) == maxReportedFindings {
		r.OmittedFindings++
		return
	}
	b, _ := json.Marshal(parts)
	h := sha256.Sum256(b)
	r.Findings = append(r.Findings, finding{Table: table, Code: code,
		KeySHA: hex.EncodeToString(h[:]), Rows: rows})
}

func canonicalUID(s string) bool {
	uid, err := strconv.ParseInt(s, 10, 64)
	return err == nil && uid > 0 && strconv.FormatInt(uid, 10) == s
}

func preflight(ctx context.Context, conn *pgx.Conn) (report, error) {
	r := newReport()
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return r, err
	}
	defer tx.Rollback(context.Background())
	var readonly string
	if err := tx.QueryRow(ctx, "SHOW transaction_read_only").Scan(&readonly); err != nil || readonly != "on" {
		return r, fmt.Errorf("read-only transaction unavailable: %v (%s)", err, readonly)
	}
	for _, spec := range tables {
		if err := verifyTable(ctx, tx, spec); err != nil {
			return r, err
		}
		if err := verifyLegacyConstraints(ctx, tx, &r, spec); err != nil {
			return r, err
		}
		if err := scanIdentity(ctx, tx, &r, spec); err != nil {
			return r, err
		}
		if err := scanProjection(ctx, tx, &r, spec); err != nil {
			return r, err
		}
	}
	if err := scanSessionOrder(ctx, tx, &r); err != nil {
		return r, err
	}
	if err := scanAnswerFK(ctx, tx, &r); err != nil {
		return r, err
	}
	if err := scanCommittedOperations(ctx, tx, &r); err != nil {
		return r, err
	}
	if err := scanTurns(ctx, tx, &r); err != nil {
		return r, err
	}
	sort.Slice(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if a.Table != b.Table {
			return a.Table < b.Table
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.KeySHA < b.KeySHA
	})
	if err := tx.Commit(ctx); err != nil {
		return r, err
	}
	return r, nil
}

func verifyTable(ctx context.Context, tx pgx.Tx, spec tableSpec) error {
	var relation *string
	if err := tx.QueryRow(ctx, "SELECT to_regclass($1)::text", spec.name).Scan(&relation); err != nil {
		return err
	}
	if relation == nil {
		return fmt.Errorf("required v1 table %s is absent", spec.name)
	}
	// Fixed column lists make a missing or renamed v1 key fail before any report.
	q := "SELECT authority_id,tenant_id,subject_id," + spec.key + " FROM " + spec.name + " LIMIT 0"
	rows, err := tx.Query(ctx, q)
	if err != nil {
		return fmt.Errorf("v1 shape of %s: %w", spec.name, err)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

func verifyLegacyConstraints(ctx context.Context, tx pgx.Tx, r *report, spec tableSpec) error {
	identity := []string{"authority_id", "tenant_id", "subject_id", "session_id"}
	var expected []constraintSpec
	switch spec.name {
	case "knowledge_answer_sessions":
		expected = append(expected, constraintSpec{"primary_key", identity, ""})
	case "knowledge_accepted_answers":
		expected = append(expected,
			constraintSpec{"primary_key", []string{"answer_id"}, ""},
			constraintSpec{"session_ordinal_unique", append(append([]string{}, identity...), "accepted_ordinal"), ""},
			constraintSpec{"session_fk", identity, "knowledge_answer_sessions"})
	case "knowledge_product_search_operations", "knowledge_tool_parents":
		expected = append(expected, constraintSpec{"primary_key", append(append([]string{}, identity...), "operation_key"), ""})
	}
	const q = `SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_constraint c
 WHERE c.conrelid=$1::regclass AND c.contype=$2 AND c.convalidated
	 AND ($4::text='' OR (c.confrelid=to_regclass($4)
	      AND (SELECT array_agg(a.attname::text ORDER BY k.ordinality) FROM unnest(c.confkey) WITH ORDINALITY k(attnum,ordinality)
	           JOIN pg_catalog.pg_attribute a ON a.attrelid=c.confrelid AND a.attnum=k.attnum)=$3::text[]))
	 AND (SELECT array_agg(a.attname::text ORDER BY k.ordinality) FROM unnest(c.conkey) WITH ORDINALITY k(attnum,ordinality)
      JOIN pg_catalog.pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=k.attnum)=$3::text[])`
	for _, item := range expected {
		kind := "p"
		switch item.kind {
		case "session_ordinal_unique":
			kind = "u"
		case "session_fk":
			kind = "f"
		}
		var exists bool
		if err := tx.QueryRow(ctx, q, spec.name, kind, item.columns, item.parent).Scan(&exists); err != nil {
			return fmt.Errorf("inspect %s %s: %w", spec.name, item.kind, err)
		}
		if !exists {
			r.add(spec.name, "legacy_constraint_missing", []string{item.kind}, 0)
		}
	}
	return nil
}

func scanIdentity(ctx context.Context, tx pgx.Tx, r *report, spec tableSpec) error {
	q := "SELECT authority_id,tenant_id,subject_id," + spec.row + " FROM " + spec.name + " ORDER BY " + spec.row
	rows, err := tx.Query(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var authority, tenant, subject, rowID string
		if err := rows.Scan(&authority, &tenant, &subject, &rowID); err != nil {
			return err
		}
		r.Rows[spec.name]++
		ref := []string{authority, tenant, subject, rowID}
		if authority != "rtw.identity" {
			r.add(spec.name, "invalid_issuer", ref, 0)
		}
		if tenant != "platform" {
			r.add(spec.name, "invalid_compatibility_slot", ref, 0)
		}
		if !canonicalUID(subject) {
			r.add(spec.name, "invalid_rtw_uid", ref, 0)
		}
	}
	return rows.Err()
}

func scanProjection(ctx context.Context, tx pgx.Tx, r *report, spec tableSpec) error {
	// Dropping tenant_id is unsafe even if the v1 composite key is unique.
	q := "SELECT authority_id,subject_id," + spec.key + ",count(*) FROM " + spec.name +
		" GROUP BY authority_id,subject_id," + spec.key + " HAVING count(*)>1 ORDER BY authority_id,subject_id," + spec.key
	rows, err := tx.Query(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return err
		}
		parts := make([]string, len(values)-1)
		for i := range parts {
			parts[i] = fmt.Sprint(values[i])
		}
		count, ok := values[len(values)-1].(int64)
		if !ok {
			return fmt.Errorf("projection count type of %s", spec.name)
		}
		r.add(spec.name, "projected_key_collision", parts, count)
	}
	return rows.Err()
}

func scanSessionOrder(ctx context.Context, tx pgx.Tx, r *report) error {
	const q = `SELECT s.authority_id,s.tenant_id,s.subject_id,s.session_id,s.last_ordinal,
 count(a.answer_id),coalesce(min(a.accepted_ordinal),0),coalesce(max(a.accepted_ordinal),0)
 FROM knowledge_answer_sessions s LEFT JOIN knowledge_accepted_answers a
 ON (a.authority_id,a.tenant_id,a.subject_id,a.session_id)=
    (s.authority_id,s.tenant_id,s.subject_id,s.session_id)
 GROUP BY s.authority_id,s.tenant_id,s.subject_id,s.session_id,s.last_ordinal
 HAVING s.last_ordinal<>count(a.answer_id) OR
   (count(a.answer_id)>0 AND (min(a.accepted_ordinal)<>1 OR max(a.accepted_ordinal)<>s.last_ordinal))
 ORDER BY s.authority_id,s.tenant_id,s.subject_id,s.session_id`
	rows, err := tx.Query(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a, t, s, session string
		var last, count, min, max int64
		if err := rows.Scan(&a, &t, &s, &session, &last, &count, &min, &max); err != nil {
			return err
		}
		r.add("knowledge_answer_sessions", "ordinal_watermark_mismatch", []string{a, t, s, session}, 0)
	}
	return rows.Err()
}

func scanAnswerFK(ctx context.Context, tx pgx.Tx, r *report) error {
	const q = `SELECT a.answer_id,a.authority_id,a.tenant_id,a.subject_id,a.session_id
 FROM knowledge_accepted_answers a LEFT JOIN knowledge_answer_sessions s
 ON (a.authority_id,a.tenant_id,a.subject_id,a.session_id)=
    (s.authority_id,s.tenant_id,s.subject_id,s.session_id)
 WHERE s.session_id IS NULL OR a.accepted_ordinal<1 OR a.accepted_ordinal>s.last_ordinal
 ORDER BY a.answer_id`
	rows, err := tx.Query(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, a, t, s, session string
		if err := rows.Scan(&id, &a, &t, &s, &session); err != nil {
			return err
		}
		r.add("knowledge_accepted_answers", "session_fk_or_ordinal_mismatch", []string{id, a, t, s, session}, 0)
	}
	return rows.Err()
}

func scanCommittedOperations(ctx context.Context, tx pgx.Tx, r *report) error {
	const q = `SELECT o.search_id,o.authority_id,o.tenant_id,o.subject_id,o.session_id,o.operation_key
 FROM knowledge_product_search_operations o LEFT JOIN knowledge_accepted_answers a ON a.answer_id=o.answer_id
 WHERE o.status='committed' AND (a.answer_id IS NULL OR a.search_id IS DISTINCT FROM o.search_id OR
 (a.authority_id,a.tenant_id,a.subject_id,a.session_id) IS DISTINCT FROM
 (o.authority_id,o.tenant_id,o.subject_id,o.session_id))
 ORDER BY o.search_id`
	rows, err := tx.Query(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, a, t, s, session, key string
		if err := rows.Scan(&id, &a, &t, &s, &session, &key); err != nil {
			return err
		}
		r.add("knowledge_product_search_operations", "committed_answer_link_mismatch", []string{id, a, t, s, session, key}, 0)
	}
	return rows.Err()
}

func scanTurns(ctx context.Context, tx pgx.Tx, r *report) error {
	var immutable bool
	const trigger = `SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_trigger t JOIN pg_catalog.pg_proc p ON p.oid=t.tgfoid
 WHERE t.tgrelid='knowledge_accepted_answers'::regclass AND t.tgname='knowledge_accepted_answer_immutable'
 AND NOT t.tgisinternal AND t.tgenabled IN ('O','A') AND p.proname='knowledge_immutable_payload'
 AND (t.tgtype & 2)<>0 AND (t.tgtype & 8)<>0 AND (t.tgtype & 16)<>0)`
	if err := tx.QueryRow(ctx, trigger).Scan(&immutable); err != nil {
		return err
	}
	if !immutable {
		r.add("knowledge_accepted_answers", "immutable_trigger_missing", []string{"knowledge_accepted_answer_immutable"}, 0)
	}
	const q = `SELECT answer_id,authority_id,tenant_id,subject_id,session_id,search_id,turn_hash,turn_json
 FROM knowledge_accepted_answers ORDER BY answer_id`
	rows, err := tx.Query(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, a, t, s, session, search, hash, raw string
		if err := rows.Scan(&id, &a, &t, &s, &session, &search, &hash, &raw); err != nil {
			return err
		}
		ref := []string{id, a, t, s, session}
		h := sha256.Sum256([]byte(raw))
		if hash != hex.EncodeToString(h[:]) {
			r.add("knowledge_accepted_answers", "turn_hash_mismatch", ref, 0)
		}
		var turn struct {
			Request struct {
				AnswerID  string `json:"AnswerID"`
				SearchID  string `json:"SearchID"`
				SessionID string `json:"SessionID"`
				Subject   struct {
					AuthorityID string `json:"authority_id"`
					TenantID    string `json:"tenant_id"`
					SubjectID   string `json:"subject_id"`
				} `json:"Subject"`
			} `json:"Request"`
		}
		if err := json.Unmarshal([]byte(raw), &turn); err != nil ||
			turn.Request.AnswerID != id || turn.Request.SearchID != search || turn.Request.SessionID != session ||
			turn.Request.Subject.AuthorityID != a || turn.Request.Subject.TenantID != t ||
			turn.Request.Subject.SubjectID != s {
			r.add("knowledge_accepted_answers", "turn_identity_mismatch", ref, 0)
		}
	}
	return rows.Err()
}
