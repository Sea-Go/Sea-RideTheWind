package model

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/telemetry"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

// RTW persists a product projection of BTW's AcceptedRootTurn. The private
// framework Session and its unvalidated model events are never copied here.
type acceptedRootTurn struct {
	Request struct {
		SearchID  string
		AnswerID  string
		Subject   types.AcceptedSubjectRef
		SessionID string
		Search    struct {
			Query        string
			Depth        string
			Intelligence string
			Snapshot     citationSnapshot
		}
	}
	Result struct {
		Search struct {
			Pack    json.RawMessage             `json:"evidence_pack"`
			Receipt types.SearchCitationReceipt `json:"citation_receipt"`
		} `json:"search"`
		AnswerID      string   `json:"answer_id"`
		Answer        string   `json:"answer"`
		Citations     []string `json:"citations"`
		SummaryStatus string   `json:"summary_status"`
	} `json:"result"`
}

type acceptedAnswerInput struct {
	turn     acceptedRootTurn
	pack     citationPack
	packHash string
	turnHash string
	byID     map[string]citationEvidence
}

func validateAcceptedAnswer(req types.CommitAcceptedAnswerReq) (acceptedAnswerInput, error) {
	var in acceptedAnswerInput
	if !citationIdentity(req.AnswerId) || !citationIdentity(req.SearchId) ||
		!validLogicalSessionID(req.SessionId) || len(req.TurnJson) == 0 || len(req.TurnJson) > 4<<20 ||
		!validAcceptedSubject(req.Subject) {
		return in, invalid("bounded answer, search, session, subject and turn required")
	}
	if err := json.Unmarshal([]byte(req.TurnJson), &in.turn); err != nil {
		return in, invalid("malformed accepted root turn")
	}
	q, out := in.turn.Request, in.turn.Result
	if q.AnswerID != req.AnswerId || q.SearchID != req.SearchId || q.SessionID != req.SessionId ||
		q.Subject != req.Subject || out.AnswerID != req.AnswerId ||
		strings.TrimSpace(q.Search.Query) == "" ||
		(q.Search.Depth != "fast" && q.Search.Depth != "detailed") ||
		(q.Search.Intelligence != "low" && q.Search.Intelligence != "medium" && q.Search.Intelligence != "high") ||
		len(out.Search.Pack) == 0 {
		return in, invalid("accepted turn identity or search request differs")
	}
	if err := json.Unmarshal(out.Search.Pack, &in.pack); err != nil || in.pack.SearchID != req.SearchId ||
		in.pack.Snapshot.ModuleID == "" || in.pack.Snapshot.ReleaseID == "" ||
		in.pack.Snapshot.Generation < 1 || !reflect.DeepEqual(in.pack.Snapshot, q.Search.Snapshot) {
		return in, invalid("accepted turn evidence pack differs from fixed search")
	}
	in.packHash = object.Hash(out.Search.Pack)
	in.turnHash = object.Hash([]byte(req.TurnJson))
	in.byID = make(map[string]citationEvidence, len(in.pack.Evidence))
	for _, evidence := range in.pack.Evidence {
		if evidence.ID == "" || in.byID[evidence.ID].ID != "" {
			return in, invalid("duplicate or empty evidence identity")
		}
		in.byID[evidence.ID] = evidence
	}
	switch out.SummaryStatus {
	case "succeeded":
		if strings.TrimSpace(out.Answer) == "" || len(out.Citations) == 0 || len(in.pack.Evidence) == 0 ||
			out.Search.Receipt.SearchId != req.SearchId || out.Search.Receipt.PackHash != in.packHash ||
			out.Search.Receipt.DurableRef == "" || (in.pack.Status != "complete" && in.pack.Status != "partial") {
			return in, invalid("succeeded answer needs exact durable evidence receipt")
		}
		seen := make(map[string]bool, len(out.Citations))
		for _, evidenceID := range out.Citations {
			if _, ok := in.byID[evidenceID]; !ok || seen[evidenceID] {
				return in, invalid("answer cites absent or duplicate evidence")
			}
			seen[evidenceID] = true
		}
	case "insufficient":
		if out.Answer != "" || len(out.Citations) != 0 || len(in.pack.Evidence) != 0 ||
			in.pack.Status != "empty" || out.Search.Receipt != (types.SearchCitationReceipt{}) {
			return in, invalid("insufficient answer cannot contain answer, citation or receipt")
		}
	default:
		return in, invalid("only accepted terminal answer states may be committed")
	}
	return in, nil
}

func validAcceptedSubject(s types.AcceptedSubjectRef) bool {
	for _, part := range []string{s.AuthorityId, s.TenantId, s.SubjectId} {
		if strings.TrimSpace(part) == "" || len(part) > 200 {
			return false
		}
	}
	return true
}

func validLogicalSessionID(sessionID string) bool {
	if strings.TrimSpace(sessionID) == "" || len(sessionID) > 200 {
		return false
	}
	for _, ch := range sessionID {
		if ch < 0x20 || ch == 0x7f {
			return false
		}
	}
	return true
}

func acceptedAnswerFromRow(row pgx.Row) (types.AcceptedAnswer, string, error) {
	var a types.AcceptedAnswer
	var at time.Time
	var hash string
	err := row.Scan(&a.AnswerId, &a.Subject.AuthorityId, &a.Subject.TenantId, &a.Subject.SubjectId,
		&a.SessionId, &a.AcceptedOrdinal, &a.SearchId, &a.Status, &hash, &a.TurnJson, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, "", ErrNotFound
	}
	if err != nil {
		return a, "", err
	}
	a.AcceptedAt = at.UTC().Format(time.RFC3339Nano)
	return a, hash, nil
}

const acceptedAnswerColumns = `answer_id,authority_id,tenant_id,subject_id,session_id,accepted_ordinal,
 search_id,status,turn_hash,turn_json,accepted_at`

func (s *Store) CommitAcceptedAnswer(ctx context.Context, req types.CommitAcceptedAnswerReq) (types.AcceptedAnswer, error) {
	return observe(ctx, s, "knowledge.answer.accept", "answer:"+req.AnswerId, req, func(ctx context.Context) (types.AcceptedAnswer, error) {
		in, err := validateAcceptedAnswer(req)
		if err != nil {
			return types.AcceptedAnswer{}, err
		}
		tx, err := s.DB.Begin(ctx)
		if err != nil {
			return types.AcceptedAnswer{}, err
		}
		defer tx.Rollback(context.Background())
		scope := []any{req.Subject.AuthorityId, req.Subject.TenantId, req.Subject.SubjectId, req.SessionId}
		_, err = tx.Exec(ctx, `INSERT INTO knowledge_answer_sessions
 (authority_id,tenant_id,subject_id,session_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, scope...)
		if err != nil {
			return types.AcceptedAnswer{}, err
		}
		var last int64
		if err = tx.QueryRow(ctx, `SELECT last_ordinal FROM knowledge_answer_sessions
 WHERE authority_id=$1 AND tenant_id=$2 AND subject_id=$3 AND session_id=$4 FOR UPDATE`, scope...).Scan(&last); err != nil {
			return types.AcceptedAnswer{}, err
		}
		previous, previousHash, err := acceptedAnswerFromRow(tx.QueryRow(ctx,
			`SELECT `+acceptedAnswerColumns+` FROM knowledge_accepted_answers WHERE answer_id=$1`, req.AnswerId))
		if err == nil {
			if previous.Subject != req.Subject || previous.SessionId != req.SessionId ||
				previous.SearchId != req.SearchId || previousHash != in.turnHash || previous.TurnJson != req.TurnJson {
				return types.AcceptedAnswer{}, conflictCode("IDEMPOTENCY_CONFLICT", "answer ID reused for a different turn")
			}
			telemetry.Replay(ctx)
			return previous, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return types.AcceptedAnswer{}, err
		}
		if in.turn.Result.SummaryStatus == "succeeded" {
			var packHash, packJSON, durableRef string
			err = tx.QueryRow(ctx, `SELECT pack_hash,pack_json,durable_ref FROM knowledge_search_citations
 WHERE search_id=$1 FOR SHARE`, req.SearchId).Scan(&packHash, &packJSON, &durableRef)
			if errors.Is(err, pgx.ErrNoRows) {
				return types.AcceptedAnswer{}, ErrNotFound
			}
			if err != nil {
				return types.AcceptedAnswer{}, err
			}
			if packHash != in.packHash || packJSON != string(in.turn.Result.Search.Pack) ||
				durableRef != in.turn.Result.Search.Receipt.DurableRef {
				return types.AcceptedAnswer{}, conflictCode("CITATION_RECEIPT_MISMATCH", "answer evidence differs from committed search citations")
			}
		}
		ordinal := last + 1
		tag, err := tx.Exec(ctx, `INSERT INTO knowledge_accepted_answers
 (answer_id,authority_id,tenant_id,subject_id,session_id,accepted_ordinal,search_id,status,turn_hash,turn_json)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(answer_id) DO NOTHING`, req.AnswerId, scope[0], scope[1], scope[2], scope[3], ordinal,
			req.SearchId, in.turn.Result.SummaryStatus, in.turnHash, req.TurnJson)
		if err != nil {
			return types.AcceptedAnswer{}, err
		}
		if tag.RowsAffected() == 0 {
			return types.AcceptedAnswer{}, conflictCode("IDEMPOTENCY_CONFLICT", "answer ID reused for a different scope or turn")
		}
		for i, evidenceID := range in.turn.Result.Citations {
			evidence := in.byID[evidenceID]
			locator, marshalErr := json.Marshal(evidence.Locator)
			if marshalErr != nil {
				return types.AcceptedAnswer{}, marshalErr
			}
			_, err = tx.Exec(ctx, `INSERT INTO knowledge_answer_citations
 (answer_id,search_id,citation_order,evidence_id,source_kind,content_id,revision_id,chunk_id,locator,quote_hash)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, req.AnswerId, req.SearchId, i,
				evidenceID, evidence.Key.SourceKind, evidence.Key.ContentID, evidence.Key.RevisionID,
				evidence.Key.ChunkID, locator, evidence.QuoteHash)
			if err != nil {
				return types.AcceptedAnswer{}, err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE knowledge_answer_sessions SET last_ordinal=$5
 WHERE authority_id=$1 AND tenant_id=$2 AND subject_id=$3 AND session_id=$4`, append(scope, ordinal)...)
		if err != nil {
			return types.AcceptedAnswer{}, err
		}
		committed, _, err := acceptedAnswerFromRow(tx.QueryRow(ctx,
			`SELECT `+acceptedAnswerColumns+` FROM knowledge_accepted_answers WHERE answer_id=$1`, req.AnswerId))
		if err != nil {
			return types.AcceptedAnswer{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return types.AcceptedAnswer{}, err
		}
		telemetry.Add(ctx, map[string]any{"accepted_ordinal": ordinal, "citation_count": len(in.turn.Result.Citations), "answer_status": in.turn.Result.SummaryStatus})
		return committed, nil
	})
}

func (s *Store) GetAcceptedAnswer(ctx context.Context, subject types.AcceptedSubjectRef, sessionID, answerID string) (types.AcceptedAnswer, error) {
	return observe(ctx, s, "knowledge.answer.get", "answer:"+answerID, struct{ AnswerId string }{answerID}, func(ctx context.Context) (types.AcceptedAnswer, error) {
		if !validAcceptedSubject(subject) || !validLogicalSessionID(sessionID) || !citationIdentity(answerID) {
			return types.AcceptedAnswer{}, invalid("answer and complete subject session required")
		}
		row := s.DB.QueryRow(ctx, `SELECT `+acceptedAnswerColumns+` FROM knowledge_accepted_answers
 WHERE answer_id=$1 AND authority_id=$2 AND tenant_id=$3 AND subject_id=$4 AND session_id=$5`,
			answerID, subject.AuthorityId, subject.TenantId, subject.SubjectId, sessionID)
		a, _, err := acceptedAnswerFromRow(row)
		return a, err
	})
}

func (s *Store) ListAcceptedAnswers(ctx context.Context, req types.ListAcceptedAnswersReq) (types.AcceptedAnswersPage, error) {
	return observe(ctx, s, "knowledge.answer.list", "answer-session:"+req.SessionId, req, func(ctx context.Context) (types.AcceptedAnswersPage, error) {
		page := types.AcceptedAnswersPage{Items: []types.AcceptedAnswer{}}
		if !validAcceptedSubject(types.AcceptedSubjectRef{AuthorityId: req.AuthorityId, TenantId: req.TenantId, SubjectId: req.SubjectId}) ||
			!validLogicalSessionID(req.SessionId) || req.AfterOrdinal < 0 || req.Limit < 1 || req.Limit > 100 {
			return page, invalid("bounded accepted answer scope, cursor and limit required")
		}
		rows, err := s.DB.Query(ctx, `SELECT `+acceptedAnswerColumns+` FROM knowledge_accepted_answers
 WHERE authority_id=$1 AND tenant_id=$2 AND subject_id=$3 AND session_id=$4 AND accepted_ordinal>$5
 ORDER BY accepted_ordinal ASC LIMIT $6`, req.AuthorityId, req.TenantId, req.SubjectId, req.SessionId,
			req.AfterOrdinal, req.Limit+1)
		if err != nil {
			return page, err
		}
		defer rows.Close()
		for rows.Next() {
			a, _, scanErr := acceptedAnswerFromRow(rows)
			if scanErr != nil {
				return page, scanErr
			}
			page.Items = append(page.Items, a)
		}
		if err = rows.Err(); err != nil {
			return page, err
		}
		if len(page.Items) > req.Limit {
			page.Items = page.Items[:req.Limit]
			page.NextOrdinal = page.Items[len(page.Items)-1].AcceptedOrdinal
		}
		return page, nil
	})
}
