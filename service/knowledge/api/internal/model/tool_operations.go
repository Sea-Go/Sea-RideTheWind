package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The budget is owned by RTW and persisted for the full authenticated parent.
// A failed or timed-out child retains its reservation; a verified success can
// refund only unused source reads and quote runes.
const (
	ToolMaxSearchCalls    = 4
	ToolMaxReadCalls      = 24
	ToolMaxQuoteRunes     = 32768
	ToolMaxReadsPerSearch = 8
	ToolMaxRunesPerSearch = 8192
	ToolParentLifetime    = 10 * time.Minute
)

type ToolParent struct {
	Subject         types.AcceptedSubjectRef
	SessionID       string
	OperationKey    string
	OperationID     string
	ModuleID        string
	Snapshot        types.SearchSnapshot
	SnapshotRef     string
	ScopeRef        string
	BudgetRef       string
	ExpiresAt       time.Time
	SearchRemaining int
	ReadRemaining   int
	QuoteRemaining  int
}

const toolParentColumns = `authority_id,tenant_id,subject_id,session_id,operation_key,operation_id,module_id,
 snapshot,snapshot_ref,scope_ref,budget_ref,expires_at,search_remaining,read_remaining,quote_remaining`

func scanToolParent(row pgx.Row) (ToolParent, error) {
	var parent ToolParent
	var snapshot []byte
	err := row.Scan(&parent.Subject.AuthorityId, &parent.Subject.TenantId, &parent.Subject.SubjectId,
		&parent.SessionID, &parent.OperationKey, &parent.OperationID, &parent.ModuleID, &snapshot,
		&parent.SnapshotRef, &parent.ScopeRef, &parent.BudgetRef, &parent.ExpiresAt,
		&parent.SearchRemaining, &parent.ReadRemaining, &parent.QuoteRemaining)
	if errors.Is(err, pgx.ErrNoRows) {
		return parent, ErrNotFound
	}
	if err != nil {
		return parent, err
	}
	if err = json.Unmarshal(snapshot, &parent.Snapshot); err != nil {
		return parent, err
	}
	return parent, nil
}

func validToolKey(value string) bool {
	return len(value) >= 8 && len(value) <= 128 && citationIdentity(value)
}

func (s *Store) GetToolParentByKey(ctx context.Context, subject types.AcceptedSubjectRef, session, key string) (ToolParent, error) {
	if !validAcceptedSubject(subject) || !validLogicalSessionID(session) || !validToolKey(key) {
		return ToolParent{}, invalid("bounded Tool parent scope and idempotency key required")
	}
	return scanToolParent(s.DB.QueryRow(ctx, `SELECT `+toolParentColumns+` FROM knowledge_tool_parents
 WHERE authority_id=$1 AND tenant_id=$2 AND subject_id=$3 AND session_id=$4 AND operation_key=$5`,
		subject.AuthorityId, subject.TenantId, subject.SubjectId, session, key))
}

func (s *Store) GetToolParent(ctx context.Context, subject types.AcceptedSubjectRef, session, operationID string) (ToolParent, error) {
	if !validAcceptedSubject(subject) || !validLogicalSessionID(session) || !citationIdentity(operationID) {
		return ToolParent{}, invalid("bounded Tool parent scope and operation ID required")
	}
	return scanToolParent(s.DB.QueryRow(ctx, `SELECT `+toolParentColumns+` FROM knowledge_tool_parents
 WHERE authority_id=$1 AND tenant_id=$2 AND subject_id=$3 AND session_id=$4 AND operation_id=$5`,
		subject.AuthorityId, subject.TenantId, subject.SubjectId, session, operationID))
}

func (s *Store) ReserveToolParent(ctx context.Context, subject types.AcceptedSubjectRef, session, key, moduleID string) (ToolParent, error) {
	if !validAcceptedSubject(subject) || !validLogicalSessionID(session) || !validToolKey(key) || !citationIdentity(moduleID) {
		return ToolParent{}, invalid("authenticated session, operation key and module required")
	}
	if s.continuousSubjectRefV2Writes {
		if err := s.requireContinuousSubjectRefV2Ready(); err != nil {
			return ToolParent{}, err
		}
		if err := v2WriteSubject(subject); err != nil {
			return ToolParent{}, err
		}
	}
	previous, err := s.GetToolParentByKey(ctx, subject, session, key)
	if err == nil {
		if previous.ModuleID != moduleID {
			return ToolParent{}, conflictCode("IDEMPOTENCY_CONFLICT", "Tool parent key reused with another module")
		}
		if s.continuousSubjectRefV2Writes {
			if err = s.ensureV2Operation(ctx, "knowledge_tool_parents_subject_v2",
				subject, session, key, "module_id", moduleID, ""); err != nil {
				return ToolParent{}, err
			}
		}
		return previous, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return ToolParent{}, err
	}
	snapshot, err := s.GetCurrentSearchSnapshot(ctx, moduleID)
	if err != nil {
		return ToolParent{}, err
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return ToolParent{}, err
	}
	digest := sha256.Sum256(raw)
	snapshotRef := "snapshot_" + hex.EncodeToString(digest[:])
	operationID := "toolop_" + uuid.NewString()
	insertSQL := `INSERT INTO knowledge_tool_parents
 (authority_id,tenant_id,subject_id,session_id,operation_key,operation_id,module_id,snapshot,
  snapshot_ref,scope_ref,budget_ref,expires_at,search_remaining,read_remaining,quote_remaining)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,clock_timestamp()+interval '10 minutes',$12,$13,$14)
 ON CONFLICT DO NOTHING`
	insertArgs := []any{subject.AuthorityId, subject.TenantId, subject.SubjectId, session, key,
		operationID, moduleID, raw, snapshotRef, "scope_" + uuid.NewString(), "budget_" + uuid.NewString(),
		ToolMaxSearchCalls, ToolMaxReadCalls, ToolMaxQuoteRunes}
	if s.continuousSubjectRefV2Writes {
		err = s.ensureV2Operation(ctx, "knowledge_tool_parents_subject_v2",
			subject, session, key, "module_id", moduleID, insertSQL, insertArgs...)
	} else {
		_, err = s.DB.Exec(ctx, insertSQL, insertArgs...)
	}
	if err != nil {
		return ToolParent{}, err
	}
	parent, err := s.GetToolParentByKey(ctx, subject, session, key)
	if err != nil {
		return ToolParent{}, err
	}
	if parent.ModuleID != moduleID {
		return ToolParent{}, conflictCode("IDEMPOTENCY_CONFLICT", "Tool parent key reused with another module")
	}
	return parent, nil
}

type ToolSearchInput struct {
	Query        string `json:"query"`
	Depth        string `json:"depth"`
	Intelligence string `json:"intelligence"`
	ContinueID   string `json:"continue_search_id,omitempty"`
	ReadCalls    int    `json:"read_calls"`
	QuoteRunes   int    `json:"quote_runes"`
}

func (in ToolSearchInput) Hash() (string, error) {
	if strings.TrimSpace(in.Query) == "" || len(in.Query) > 4096 ||
		(in.Depth != "fast" && in.Depth != "detailed") ||
		(in.Intelligence != "low" && in.Intelligence != "medium" && in.Intelligence != "high") ||
		in.ContinueID != "" || in.ReadCalls < 1 || in.ReadCalls > ToolMaxReadsPerSearch ||
		in.QuoteRunes < 1 || in.QuoteRunes > ToolMaxRunesPerSearch {
		return "", invalid("bounded Tool search request required; continuation is not yet available")
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

type ToolSearch struct {
	OperationID   string
	Key           string
	RequestHash   string
	Input         ToolSearchInput
	SearchID      string
	ReservedReads int
	ReservedRunes int
	Status        string
	LeaseToken    string
	Attempt       int64
	Result        json.RawMessage
	LastErrorCode string
}

const toolSearchColumns = `operation_id,operation_key,request_hash,request_json,search_id,
 reserved_reads,reserved_runes,status,COALESCE(lease_token,''),attempt,result_json,last_error_code`

func scanToolSearch(row pgx.Row) (ToolSearch, error) {
	var search ToolSearch
	var raw []byte
	err := row.Scan(&search.OperationID, &search.Key, &search.RequestHash, &raw, &search.SearchID,
		&search.ReservedReads, &search.ReservedRunes, &search.Status, &search.LeaseToken,
		&search.Attempt, &search.Result, &search.LastErrorCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return search, ErrNotFound
	}
	if err != nil {
		return search, err
	}
	if err = json.Unmarshal(raw, &search.Input); err != nil {
		return search, err
	}
	return search, nil
}

func (s *Store) GetToolSearch(ctx context.Context, parent ToolParent, searchID string) (ToolSearch, error) {
	if !citationIdentity(parent.OperationID) || !citationIdentity(searchID) {
		return ToolSearch{}, invalid("Tool parent and search ID required")
	}
	return scanToolSearch(s.DB.QueryRow(ctx, `SELECT `+toolSearchColumns+` FROM knowledge_tool_searches
 WHERE operation_id=$1 AND search_id=$2`, parent.OperationID, searchID))
}

func (s *Store) ReserveToolSearch(ctx context.Context, parent ToolParent, key string, input ToolSearchInput) (ToolSearch, error) {
	hash, err := input.Hash()
	if err != nil {
		return ToolSearch{}, err
	}
	if !validToolKey(key) || !citationIdentity(parent.OperationID) {
		return ToolSearch{}, invalid("bounded Tool search idempotency key required")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return ToolSearch{}, err
	}
	defer tx.Rollback(context.Background())
	var expires time.Time
	var searches, reads, runes int
	err = tx.QueryRow(ctx, `SELECT expires_at,search_remaining,read_remaining,quote_remaining
 FROM knowledge_tool_parents WHERE operation_id=$1 AND authority_id=$2 AND tenant_id=$3
 AND subject_id=$4 AND session_id=$5 FOR UPDATE`, parent.OperationID, parent.Subject.AuthorityId,
		parent.Subject.TenantId, parent.Subject.SubjectId, parent.SessionID).Scan(&expires, &searches, &reads, &runes)
	if errors.Is(err, pgx.ErrNoRows) {
		return ToolSearch{}, ErrNotFound
	}
	if err != nil {
		return ToolSearch{}, err
	}
	previous, err := scanToolSearch(tx.QueryRow(ctx, `SELECT `+toolSearchColumns+` FROM knowledge_tool_searches
 WHERE operation_id=$1 AND operation_key=$2`, parent.OperationID, key))
	if err == nil {
		if previous.RequestHash != hash || !reflect.DeepEqual(previous.Input, input) {
			return ToolSearch{}, conflictCode("IDEMPOTENCY_CONFLICT", "Tool search key reused with different input")
		}
		return previous, tx.Commit(ctx)
	}
	if !errors.Is(err, ErrNotFound) {
		return ToolSearch{}, err
	}
	if time.Now().After(expires) {
		return ToolSearch{}, ErrUnavailable
	}
	if searches < 1 || reads < input.ReadCalls || runes < input.QuoteRunes {
		return ToolSearch{}, conflictCode("BUDGET_EXHAUSTED", "Tool parent cumulative budget exhausted")
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return ToolSearch{}, err
	}
	searchID := "search_" + uuid.NewString()
	if _, err = tx.Exec(ctx, `INSERT INTO knowledge_tool_searches
 (operation_id,operation_key,request_hash,request_json,search_id,reserved_reads,reserved_runes,status)
 VALUES($1,$2,$3,$4,$5,$6,$7,'pending')`, parent.OperationID, key, hash, raw,
		searchID, input.ReadCalls, input.QuoteRunes); err != nil {
		return ToolSearch{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE knowledge_tool_parents SET search_remaining=search_remaining-1,
 read_remaining=read_remaining-$2,quote_remaining=quote_remaining-$3 WHERE operation_id=$1`,
		parent.OperationID, input.ReadCalls, input.QuoteRunes); err != nil {
		return ToolSearch{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ToolSearch{}, err
	}
	return s.GetToolSearch(ctx, parent, searchID)
}

func (s *Store) ClaimToolSearch(ctx context.Context, search ToolSearch) (ToolSearch, bool, error) {
	token := uuid.NewString()
	claimed, err := scanToolSearch(s.DB.QueryRow(ctx, `UPDATE knowledge_tool_searches
 SET status='running',lease_token=$3,lease_until=clock_timestamp()+interval '2 minutes',
 attempt=attempt+1,last_error_code='',updated_at=clock_timestamp()
 WHERE operation_id=$1 AND search_id=$2 AND status<>'complete'
 AND (lease_until IS NULL OR lease_until<clock_timestamp())
 RETURNING `+toolSearchColumns, search.OperationID, search.SearchID, token))
	if errors.Is(err, ErrNotFound) {
		current, readErr := s.GetToolSearch(ctx, ToolParent{OperationID: search.OperationID}, search.SearchID)
		return current, false, readErr
	}
	return claimed, err == nil, err
}

func (s *Store) FailToolSearch(ctx context.Context, search ToolSearch, code string) error {
	if len(code) > 100 {
		code = "TOOLS_UPSTREAM_UNAVAILABLE"
	}
	_, err := s.DB.Exec(ctx, `UPDATE knowledge_tool_searches SET status='failed',lease_token=NULL,
 lease_until=NULL,last_error_code=$4,updated_at=clock_timestamp()
 WHERE operation_id=$1 AND search_id=$2 AND status='running' AND lease_token=$3`,
		search.OperationID, search.SearchID, search.LeaseToken, code)
	return err
}

// CompleteToolSearch verifies the product projection under the publication
// lock and releases unused reserved work in that same transaction.
func (s *Store) CompleteToolSearch(ctx context.Context, parent ToolParent, search ToolSearch,
	result json.RawMessage, usedReads, usedRunes int, allowLower bool) error {
	if search.LeaseToken == "" || len(result) == 0 || len(result) > 1<<20 || !json.Valid(result) ||
		parent.OperationID != search.OperationID ||
		usedReads < 0 || usedReads > search.ReservedReads || usedRunes < 0 || usedRunes > search.ReservedRunes {
		return invalid("bounded verified Tool result and usage required")
	}
	var projected types.ToolSearchResult
	if err := json.Unmarshal(result, &projected); err != nil ||
		projected.Usage.ReadCalls != usedReads || projected.Usage.QuoteRunes != usedRunes {
		return ErrArtifactUnavailable
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	// Parent then module is the same lock order as rereads. A withdrawal
	// cannot commit between final evidence verification and this commit.
	var live bool
	if err = tx.QueryRow(ctx, `SELECT expires_at>clock_timestamp() FROM knowledge_tool_parents
 WHERE operation_id=$1 AND authority_id=$2 AND tenant_id=$3 AND subject_id=$4 AND session_id=$5 FOR UPDATE`,
		parent.OperationID, parent.Subject.AuthorityId, parent.Subject.TenantId, parent.Subject.SubjectId,
		parent.SessionID).Scan(&live); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if !live {
		return ErrUnavailable
	}
	if _, err = tx.Exec(ctx, `SELECT 1 FROM knowledge_modules WHERE id=$1 FOR UPDATE`,
		parent.ModuleID); err != nil {
		return err
	}
	if err = s.VerifyToolSearchResult(ctx, parent, search, projected, allowLower); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE knowledge_tool_searches SET status='complete',lease_token=NULL,
 lease_until=NULL,result_json=$4,last_error_code='',updated_at=clock_timestamp()
 WHERE operation_id=$1 AND search_id=$2 AND status='running' AND lease_token=$3`,
		search.OperationID, search.SearchID, search.LeaseToken, result)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	_, err = tx.Exec(ctx, `UPDATE knowledge_tool_parents SET read_remaining=read_remaining+$2,
 quote_remaining=quote_remaining+$3 WHERE operation_id=$1`, search.OperationID,
		search.ReservedReads-usedReads, search.ReservedRunes-usedRunes)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) CheckToolSearchSchema(ctx context.Context) error {
	for _, query := range []string{
		`SELECT operation_id,snapshot_ref,budget_ref,search_remaining,read_remaining,quote_remaining FROM knowledge_tool_parents LIMIT 0`,
		`SELECT search_id,request_hash,reserved_reads,reserved_runes,status,result_json FROM knowledge_tool_searches LIMIT 0`,
		`SELECT search_id,evidence_id,result_json FROM knowledge_tool_reads LIMIT 0`,
	} {
		rows, err := s.DB.Query(ctx, query)
		if err != nil {
			return fmt.Errorf("Tool search schema unavailable: %w", err)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
	}
	return nil
}
