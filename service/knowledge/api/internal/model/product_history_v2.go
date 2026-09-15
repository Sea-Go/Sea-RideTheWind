package model

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/telemetry"
	"sea-try-go/service/knowledge/api/internal/types"
	"sea-try-go/service/knowledge/internal/subjectrefturn"

	"github.com/jackc/pgx/v5"
)

// v2 reads the immutable v1 history under one PostgreSQL snapshot. It does not
// rewrite the historical turn, its hash, or the legacy storage key.
func (s *Store) GetVerifiedProductAcceptedAnswer(ctx context.Context, subject types.AcceptedSubjectRef,
	sessionID, answerID string) (types.AcceptedAnswer, error) {
	return observe(ctx, s, "knowledge.answer.v2.get", "answer:"+answerID,
		struct{ AnswerId string }{answerID}, func(ctx context.Context) (types.AcceptedAnswer, error) {
			if !validProductHistorySubject(subject) || !validLogicalSessionID(sessionID) || !citationIdentity(answerID) {
				return types.AcceptedAnswer{}, invalid("authenticated canonical subject, session and answer required")
			}
			telemetry.Add(ctx, map[string]any{"subject_id": subject.SubjectId})
			tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if err != nil {
				return types.AcceptedAnswer{}, err
			}
			defer tx.Rollback(context.Background())
			if err = rejectProductHistoryCollision(ctx, tx, subject, sessionID); err != nil {
				return types.AcceptedAnswer{}, err
			}
			a, hash, err := acceptedAnswerFromRow(tx.QueryRow(ctx, `SELECT `+acceptedAnswerColumns+` FROM knowledge_accepted_answers
 WHERE answer_id=$1 AND authority_id=$2 AND tenant_id=$3 AND subject_id=$4 AND session_id=$5`,
				answerID, subject.AuthorityId, subject.TenantId, subject.SubjectId, sessionID))
			if err != nil {
				return types.AcceptedAnswer{}, err
			}
			if err = verifyProductHistoryRow(ctx, tx, a, hash, subject); err != nil {
				return types.AcceptedAnswer{}, err
			}
			return a, tx.Commit(ctx)
		})
}

func (s *Store) ListVerifiedProductAcceptedAnswers(ctx context.Context, req types.ListAcceptedAnswersReq) (types.AcceptedAnswersPage, error) {
	return observe(ctx, s, "knowledge.answer.v2.list", "answer-session:"+req.SessionId, req,
		func(ctx context.Context) (types.AcceptedAnswersPage, error) {
			page := types.AcceptedAnswersPage{Items: []types.AcceptedAnswer{}}
			subject := types.AcceptedSubjectRef{AuthorityId: req.AuthorityId, TenantId: req.TenantId, SubjectId: req.SubjectId}
			if !validProductHistorySubject(subject) || !validLogicalSessionID(req.SessionId) ||
				req.AfterOrdinal < 0 || req.Limit < 1 || req.Limit > 100 {
				return page, invalid("authenticated canonical scope, cursor and limit required")
			}
			telemetry.Add(ctx, map[string]any{"subject_id": subject.SubjectId})
			tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if err != nil {
				return page, err
			}
			defer tx.Rollback(context.Background())
			if err = rejectProductHistoryCollision(ctx, tx, subject, req.SessionId); err != nil {
				return page, err
			}
			rows, err := tx.Query(ctx, `SELECT `+acceptedAnswerColumns+` FROM knowledge_accepted_answers
 WHERE authority_id=$1 AND tenant_id=$2 AND subject_id=$3 AND session_id=$4 AND accepted_ordinal>$5
 ORDER BY accepted_ordinal ASC LIMIT $6`, subject.AuthorityId, subject.TenantId, subject.SubjectId,
				req.SessionId, req.AfterOrdinal, req.Limit+1)
			if err != nil {
				return page, err
			}
			// pgx permits only one active query on a transaction. Collect the bounded
			// rows before checking their durable evidence in the same snapshot.
			type historicalRow struct {
				answer types.AcceptedAnswer
				hash   string
			}
			collected := make([]historicalRow, 0, req.Limit+1)
			for rows.Next() {
				a, hash, scanErr := acceptedAnswerFromRow(rows)
				if scanErr != nil {
					rows.Close()
					return page, scanErr
				}
				collected = append(collected, historicalRow{a, hash})
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return page, err
			}
			for _, row := range collected {
				if err = verifyProductHistoryRow(ctx, tx, row.answer, row.hash, subject); err != nil {
					return page, err
				}
				page.Items = append(page.Items, row.answer)
			}
			if len(page.Items) > req.Limit {
				page.Items = page.Items[:req.Limit]
				page.NextOrdinal = page.Items[len(page.Items)-1].AcceptedOrdinal
			}
			return page, tx.Commit(ctx)
		})
}

func validProductHistorySubject(subject types.AcceptedSubjectRef) bool {
	uid, err := strconv.ParseInt(subject.SubjectId, 10, 64)
	return subject.AuthorityId == "rtw.identity" && subject.TenantId == "platform" &&
		err == nil && uid > 0 && subject.SubjectId == strconv.FormatInt(uid, 10)
}

func rejectProductHistoryCollision(ctx context.Context, tx pgx.Tx, subject types.AcceptedSubjectRef, sessionID string) error {
	var collided bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (
 SELECT 1 FROM knowledge_accepted_answers
 WHERE authority_id=$1 AND subject_id=$2 AND session_id=$3
 GROUP BY accepted_ordinal HAVING count(*)>1)`,
		subject.AuthorityId, subject.SubjectId, sessionID).Scan(&collided)
	if err != nil {
		return err
	}
	if collided {
		return conflictCode("SUBJECTREF_PROJECTION_COLLISION", "legacy slots collide under the v2 product history key")
	}
	return nil
}

func verifyProductHistoryRow(ctx context.Context, tx pgx.Tx, a types.AcceptedAnswer,
	storedHash string, subject types.AcceptedSubjectRef) error {
	if a.Subject != subject || a.AcceptedOrdinal < 1 || !citationHash(storedHash) ||
		object.Hash([]byte(a.TurnJson)) != storedHash || len(a.TurnJson) == 0 || len(a.TurnJson) > 4<<20 {
		return ErrArtifactUnavailable
	}
	ambiguous, err := subjectrefturn.AmbiguousFrozenTurnKeys([]byte(a.TurnJson),
		subjectrefturn.ScanOptions{RejectAnyExactDuplicate: true})
	if err != nil || ambiguous {
		return ErrArtifactUnavailable
	}
	// The old turn's Subject must be exactly the frozen v1 wire. A stray v2
	// tenant/realm/issuer cannot be accepted through case-insensitive Unmarshal.
	var root map[string]json.RawMessage
	var request map[string]json.RawMessage
	var rawSubject map[string]json.RawMessage
	if json.Unmarshal([]byte(a.TurnJson), &root) != nil || json.Unmarshal(root["Request"], &request) != nil ||
		json.Unmarshal(request["Subject"], &rawSubject) != nil || len(rawSubject) != 3 {
		return ErrArtifactUnavailable
	}
	for _, key := range []string{"authority_id", "tenant_id", "subject_id"} {
		if _, ok := rawSubject[key]; !ok {
			return ErrArtifactUnavailable
		}
	}
	verified, err := validateAcceptedAnswer(types.CommitAcceptedAnswerReq{AnswerId: a.AnswerId,
		SearchId: a.SearchId, Subject: subject, SessionId: a.SessionId, TurnJson: a.TurnJson})
	if err != nil || verified.turnHash != storedHash || verified.turn.Result.SummaryStatus != a.Status {
		return ErrArtifactUnavailable
	}
	if a.Status == "insufficient" {
		return nil
	}
	var packHash, packJSON, durableRef string
	err = tx.QueryRow(ctx, `SELECT pack_hash,pack_json,durable_ref FROM knowledge_search_citations
 WHERE search_id=$1`, a.SearchId).Scan(&packHash, &packJSON, &durableRef)
	if err != nil || packHash != verified.packHash || packJSON != string(verified.turn.Result.Search.Pack) ||
		durableRef != verified.turn.Result.Search.Receipt.DurableRef {
		return ErrArtifactUnavailable
	}
	rows, err := tx.Query(ctx, `SELECT citation_order,evidence_id,search_id,source_kind,content_id,revision_id,chunk_id,locator,quote_hash
 FROM knowledge_answer_citations WHERE answer_id=$1 ORDER BY citation_order ASC`, a.AnswerId)
	if err != nil {
		return err
	}
	count := 0
	for rows.Next() {
		var order int
		var id, citationSearchID, sourceKind, contentID, revisionID, chunkID, quoteHash string
		var locatorRaw []byte
		if err = rows.Scan(&order, &id, &citationSearchID, &sourceKind, &contentID, &revisionID, &chunkID, &locatorRaw, &quoteHash); err != nil {
			rows.Close()
			return err
		}
		if count >= len(verified.turn.Result.Citations) || order != count || id != verified.turn.Result.Citations[count] ||
			citationSearchID != a.SearchId {
			rows.Close()
			return ErrArtifactUnavailable
		}
		evidence := verified.byID[id]
		var locator types.CitationLocation
		if json.Unmarshal(locatorRaw, &locator) != nil || evidence.Key != (citationKey{sourceKind, contentID, revisionID, chunkID}) ||
			!reflect.DeepEqual(locator, evidence.Locator) || quoteHash != evidence.QuoteHash ||
			object.Hash([]byte(evidence.Quote)) != quoteHash || id != citationEvidenceID(a.SearchId, evidence.Key, quoteHash) {
			rows.Close()
			return ErrArtifactUnavailable
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if count != len(verified.turn.Result.Citations) {
		return ErrArtifactUnavailable
	}
	return nil
}
