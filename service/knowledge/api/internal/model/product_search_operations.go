package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ProductSearchInput is also the four-field BTW request and request-hash wire.
// Its JSON field order is part of the signed H02 contract.
type ProductSearchInput struct {
	ModuleID     string `json:"module_id"`
	Query        string `json:"query"`
	Depth        string `json:"depth"`
	Intelligence string `json:"intelligence"`
}

func (in ProductSearchInput) Hash() (string, error) {
	if !citationIdentity(in.ModuleID) || strings.TrimSpace(in.Query) == "" || len(in.Query) > 4096 ||
		(in.Depth != "fast" && in.Depth != "detailed") ||
		(in.Intelligence != "low" && in.Intelligence != "medium" && in.Intelligence != "high") {
		return "", invalid("bounded module, query, depth and intelligence required")
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

type ProductSearchOperation struct {
	Subject       types.AcceptedSubjectRef
	SessionID     string
	Key           string
	RequestHash   string
	Search        ProductSearchInput
	Snapshot      types.SearchSnapshot
	SearchID      string
	AnswerID      string
	Status        string
	Attempt       int64
	LeaseToken    string
	LastErrorCode string
}

const productSearchColumns = `authority_id,tenant_id,subject_id,session_id,operation_key,
 request_hash,request_json,snapshot,search_id,answer_id,status,attempt,COALESCE(lease_token,''),last_error_code`

func scanProductSearch(row pgx.Row) (ProductSearchOperation, error) {
	var op ProductSearchOperation
	var inputRaw, snapshotRaw []byte
	err := row.Scan(&op.Subject.AuthorityId, &op.Subject.TenantId, &op.Subject.SubjectId,
		&op.SessionID, &op.Key, &op.RequestHash, &inputRaw, &snapshotRaw,
		&op.SearchID, &op.AnswerID, &op.Status, &op.Attempt, &op.LeaseToken, &op.LastErrorCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return op, ErrNotFound
	}
	if err != nil {
		return op, err
	}
	if err = json.Unmarshal(inputRaw, &op.Search); err != nil {
		return op, err
	}
	if err = json.Unmarshal(snapshotRaw, &op.Snapshot); err != nil {
		return op, err
	}
	return op, nil
}

func productSearchScope(subject types.AcceptedSubjectRef, session string) []any {
	return []any{subject.AuthorityId, subject.TenantId, subject.SubjectId, session}
}

func (s *Store) GetProductSearchByKey(ctx context.Context, subject types.AcceptedSubjectRef, session, key string) (ProductSearchOperation, error) {
	if !validAcceptedSubject(subject) || !validLogicalSessionID(session) ||
		len(key) < 8 || len(key) > 128 || !citationIdentity(key) {
		return ProductSearchOperation{}, invalid("bounded product search scope and key required")
	}
	return scanProductSearch(s.DB.QueryRow(ctx, `SELECT `+productSearchColumns+` FROM knowledge_product_search_operations
 WHERE authority_id=$1 AND tenant_id=$2 AND subject_id=$3 AND session_id=$4 AND operation_key=$5`,
		append(productSearchScope(subject, session), key)...))
}

func (s *Store) GetProductSearchByID(ctx context.Context, subject types.AcceptedSubjectRef, session, searchID string) (ProductSearchOperation, error) {
	if !validAcceptedSubject(subject) || !validLogicalSessionID(session) || !citationIdentity(searchID) {
		return ProductSearchOperation{}, invalid("bounded product search scope and ID required")
	}
	return scanProductSearch(s.DB.QueryRow(ctx, `SELECT `+productSearchColumns+` FROM knowledge_product_search_operations
 WHERE authority_id=$1 AND tenant_id=$2 AND subject_id=$3 AND session_id=$4 AND search_id=$5`,
		append(productSearchScope(subject, session), searchID)...))
}

// ReserveProductSearch reads an old key first; only a new key observes the
// moving publication. Concurrent creators converge on one stored operation.
func (s *Store) ReserveProductSearch(ctx context.Context, subject types.AcceptedSubjectRef, session, key string,
	input ProductSearchInput) (ProductSearchOperation, error) {
	hash, err := input.Hash()
	if err != nil {
		return ProductSearchOperation{}, err
	}
	if s.continuousSubjectRefV2Writes {
		if err = s.requireContinuousSubjectRefV2Ready(); err != nil {
			return ProductSearchOperation{}, err
		}
		if err = v2WriteSubject(subject); err != nil {
			return ProductSearchOperation{}, err
		}
	}
	previous, err := s.GetProductSearchByKey(ctx, subject, session, key)
	if err == nil {
		if previous.RequestHash != hash || previous.Search != input {
			return ProductSearchOperation{}, conflictCode("IDEMPOTENCY_CONFLICT", "product search key reused with different input")
		}
		if s.continuousSubjectRefV2Writes {
			if err = s.ensureV2Operation(ctx, "knowledge_product_search_operations_subject_v2",
				subject, session, key, "request_hash", hash, ""); err != nil {
				return ProductSearchOperation{}, err
			}
		}
		return previous, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return ProductSearchOperation{}, err
	}
	snapshot, err := s.GetCurrentSearchSnapshot(ctx, input.ModuleID)
	if err != nil {
		return ProductSearchOperation{}, err
	}
	inputRaw, err := json.Marshal(input)
	if err != nil {
		return ProductSearchOperation{}, err
	}
	snapshotRaw, err := json.Marshal(snapshot)
	if err != nil {
		return ProductSearchOperation{}, err
	}
	insertSQL := `INSERT INTO knowledge_product_search_operations
 (authority_id,tenant_id,subject_id,session_id,operation_key,request_hash,request_json,snapshot,
  search_id,answer_id,status)
	 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'pending') ON CONFLICT DO NOTHING`
	insertArgs := []any{subject.AuthorityId, subject.TenantId, subject.SubjectId, session, key, hash, inputRaw, snapshotRaw,
		"search_" + uuid.NewString(), "answer_" + uuid.NewString()}
	if s.continuousSubjectRefV2Writes {
		err = s.ensureV2Operation(ctx, "knowledge_product_search_operations_subject_v2",
			subject, session, key, "request_hash", hash, insertSQL, insertArgs...)
	} else {
		_, err = s.DB.Exec(ctx, insertSQL, insertArgs...)
	}
	if err != nil {
		return ProductSearchOperation{}, err
	}
	op, err := s.GetProductSearchByKey(ctx, subject, session, key)
	if err != nil {
		return ProductSearchOperation{}, err
	}
	if op.RequestHash != hash || op.Search != input {
		return ProductSearchOperation{}, conflictCode("IDEMPOTENCY_CONFLICT", "product search key reused with different input")
	}
	return op, nil
}

// ClaimProductSearch uses a bounded lease so a crash or cancellation does not
// leave an operation permanently running. A retry first reconciles AnswerID.
func (s *Store) ClaimProductSearch(ctx context.Context, op ProductSearchOperation) (ProductSearchOperation, bool, error) {
	token := uuid.NewString()
	claimed, err := scanProductSearch(s.DB.QueryRow(ctx, `UPDATE knowledge_product_search_operations
 SET status='running',attempt=attempt+1,lease_token=$6,lease_until=clock_timestamp()+interval '2 minutes',
 updated_at=clock_timestamp(),last_error_code=''
 WHERE authority_id=$1 AND tenant_id=$2 AND subject_id=$3 AND session_id=$4 AND search_id=$5
 AND status<>'committed' AND (lease_until IS NULL OR lease_until<clock_timestamp())
 RETURNING `+productSearchColumns,
		op.Subject.AuthorityId, op.Subject.TenantId, op.Subject.SubjectId, op.SessionID, op.SearchID, token))
	if errors.Is(err, ErrNotFound) {
		current, readErr := s.GetProductSearchByID(ctx, op.Subject, op.SessionID, op.SearchID)
		return current, false, readErr
	}
	return claimed, err == nil, err
}

func (s *Store) FailProductSearch(ctx context.Context, op ProductSearchOperation, code string) error {
	if len(code) > 100 {
		code = "SEARCH_UNAVAILABLE"
	}
	_, err := s.DB.Exec(ctx, `UPDATE knowledge_product_search_operations
 SET status='failed',lease_token=NULL,lease_until=NULL,last_error_code=$7,updated_at=clock_timestamp()
 WHERE authority_id=$1 AND tenant_id=$2 AND subject_id=$3 AND session_id=$4 AND search_id=$5
 AND status='running' AND lease_token=$6`, op.Subject.AuthorityId, op.Subject.TenantId, op.Subject.SubjectId,
		op.SessionID, op.SearchID, op.LeaseToken, code)
	return err
}

// CompleteProductSearch is idempotent after a separately verified accepted
// answer; the SQL predicate still requires that exact answer and scope.
func (s *Store) CompleteProductSearch(ctx context.Context, op ProductSearchOperation) error {
	tag, err := s.DB.Exec(ctx, `UPDATE knowledge_product_search_operations o
 SET status='committed',lease_token=NULL,lease_until=NULL,last_error_code='',updated_at=clock_timestamp()
 WHERE o.authority_id=$1 AND o.tenant_id=$2 AND o.subject_id=$3 AND o.session_id=$4 AND o.search_id=$5
 AND o.answer_id=$6 AND EXISTS (SELECT 1 FROM knowledge_accepted_answers a WHERE a.answer_id=o.answer_id
  AND a.search_id=o.search_id AND a.authority_id=o.authority_id AND a.tenant_id=o.tenant_id
  AND a.subject_id=o.subject_id AND a.session_id=o.session_id)`,
		op.Subject.AuthorityId, op.Subject.TenantId, op.Subject.SubjectId, op.SessionID, op.SearchID, op.AnswerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}
