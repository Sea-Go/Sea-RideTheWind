package model

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

const reviewerAuthority = "ridethewind.knowledge.admin"

func lockReviewerRegistration(ctx context.Context, tx pgx.Tx, keyID, publicHex string) error {
	locks := [2]string{"reviewer-key/id/" + keyID, "reviewer-key/public/" + publicHex}
	if locks[1] < locks[0] {
		locks[0], locks[1] = locks[1], locks[0]
	}
	for _, lock := range locks {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", lock); err != nil {
			return err
		}
	}
	return nil
}

func validReviewerKey(keyID, publicHex string) bool {
	if !citationIdentity(keyID) || len(keyID) > 128 || len(publicHex) != ed25519.PublicKeySize*2 || strings.ToLower(publicHex) != publicHex {
		return false
	}
	decoded, err := hex.DecodeString(publicHex)
	return err == nil && len(decoded) == ed25519.PublicKeySize
}

func (s *Store) CheckGroundingReviewSchema(ctx context.Context) error {
	for _, statement := range []string{
		"SELECT key_id,reviewer_authority,reviewer_id,data_kind,public_key_ed25519_hex,registered_at FROM knowledge_reviewer_keys LIMIT 0",
		"SELECT key_id,revoked_at,actor_id,reason,event_id FROM knowledge_reviewer_key_revocations LIMIT 0",
		"SELECT case_sha256,key_id,case_json,review_json,review_sha256,answer_id,search_id,event_id,event_json,event_sha256 FROM knowledge_answer_grounding_reviews LIMIT 0",
	} {
		rows, err := s.DB.Query(ctx, statement)
		if err != nil {
			return err
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RegisterReviewerKey(ctx context.Context, actor string, req types.RegisterReviewerKeyReq) (types.ReviewerKeyRecord, error) {
	return observe(ctx, s, "knowledge.reviewer.key.register", operationID("reviewer-key/register/"+actor, req.IdempotencyKey), req,
		func(ctx context.Context) (types.ReviewerKeyRecord, error) {
			if !validLogicalSessionID(actor) || !validReviewerKey(req.KeyId, req.PublicKeyEd25519Hex) ||
				(req.DataKind != "human_admin" && req.DataKind != "synthetic_fixture") ||
				!citationIdentity(req.IdempotencyKey) || len(req.IdempotencyKey) < 8 || len(req.IdempotencyKey) > 200 {
				return types.ReviewerKeyRecord{}, invalid("bounded reviewer key, kind and idempotency key required")
			}
			return command(ctx, s, "reviewer-key/register/"+actor, req.IdempotencyKey, req,
				func(tx pgx.Tx) (types.ReviewerKeyRecord, error) {
					// Different administrators and idempotency keys can still race on
					// the same key ID or public key. Lock both registry identities in
					// stable order so the loser receives a domain conflict rather than
					// a storage-specific unique-constraint error.
					if err := lockReviewerRegistration(ctx, tx, req.KeyId, req.PublicKeyEd25519Hex); err != nil {
						return types.ReviewerKeyRecord{}, err
					}
					var exists bool
					if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM knowledge_reviewer_keys
 WHERE key_id=$1 OR public_key_ed25519_hex=$2)`, req.KeyId, req.PublicKeyEd25519Hex).Scan(&exists); err != nil {
						return types.ReviewerKeyRecord{}, err
					}
					if exists {
						return types.ReviewerKeyRecord{}, conflict("reviewer key identity already registered")
					}
					var at time.Time
					if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&at); err != nil {
						return types.ReviewerKeyRecord{}, err
					}
					out := types.ReviewerKeyRecord{SchemaVersion: "sea.rtw.reviewer-key.v1", KeyId: req.KeyId,
						ReviewerAuthority: reviewerAuthority, ReviewerId: actor, DataKind: req.DataKind,
						PublicKeyEd25519Hex: req.PublicKeyEd25519Hex, RegisteredAt: at.UTC().Format(time.RFC3339Nano),
						Status: "active", RegistryRevision: 1}
					_, err := tx.Exec(ctx, `INSERT INTO knowledge_reviewer_keys
 (key_id,reviewer_authority,reviewer_id,data_kind,public_key_ed25519_hex,registered_at)
 VALUES($1,$2,$3,$4,$5,$6)`, out.KeyId, out.ReviewerAuthority, out.ReviewerId,
						out.DataKind, out.PublicKeyEd25519Hex, at)
					if err != nil {
						return types.ReviewerKeyRecord{}, err
					}
					event, _, err := emitWithVersion(ctx, tx, "knowledge.reviewer.key.registered.v1",
						"reviewer-key/"+req.KeyId, 1, out)
					if err != nil {
						return types.ReviewerKeyRecord{}, err
					}
					out.RegistrationEventId = event.EventID
					return out, nil
				})
		})
}

func (s *Store) RevokeReviewerKey(ctx context.Context, actor string, req types.RevokeReviewerKeyReq) (types.ReviewerKeyRecord, error) {
	return observe(ctx, s, "knowledge.reviewer.key.revoke", operationID("reviewer-key/revoke/"+actor, req.IdempotencyKey), req,
		func(ctx context.Context) (types.ReviewerKeyRecord, error) {
			if !validLogicalSessionID(actor) || !citationIdentity(req.KeyId) || len(req.KeyId) > 128 ||
				!citationIdentity(req.IdempotencyKey) || len(req.IdempotencyKey) < 8 || len(req.IdempotencyKey) > 200 ||
				strings.TrimSpace(req.Reason) == "" || len(req.Reason) > 2000 {
				return types.ReviewerKeyRecord{}, invalid("bounded revocation key and reason required")
			}
			return command(ctx, s, "reviewer-key/revoke/"+actor, req.IdempotencyKey, req,
				func(tx pgx.Tx) (types.ReviewerKeyRecord, error) {
					out, err := reviewerKey(ctx, tx, req.KeyId, true)
					if err != nil {
						return types.ReviewerKeyRecord{}, err
					}
					if out.Status != "active" {
						return types.ReviewerKeyRecord{}, conflict("reviewer key already revoked")
					}
					var at time.Time
					if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&at); err != nil {
						return types.ReviewerKeyRecord{}, err
					}
					out.RevokedAt, out.Status, out.RegistryRevision = at.UTC().Format(time.RFC3339Nano), "revoked", 2
					event, _, err := emitWithVersion(ctx, tx, "knowledge.reviewer.key.revoked.v1",
						"reviewer-key/"+req.KeyId, 2, struct {
							KeyID      string `json:"key_id"`
							ReviewerID string `json:"reviewer_id"`
							ActorID    string `json:"actor_id"`
							RevokedAt  string `json:"revoked_at"`
							Reason     string `json:"reason"`
						}{req.KeyId, out.ReviewerId, actor, out.RevokedAt, req.Reason})
					if err != nil {
						return types.ReviewerKeyRecord{}, err
					}
					_, err = tx.Exec(ctx, `INSERT INTO knowledge_reviewer_key_revocations
 (key_id,revoked_at,actor_id,reason,event_id) VALUES($1,$2,$3,$4,$5)`,
						req.KeyId, at, actor, req.Reason, event.EventID)
					if err != nil {
						return types.ReviewerKeyRecord{}, err
					}
					out.RevocationEventId = event.EventID
					return out, nil
				})
		})
}

type reviewerKeyQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func reviewerKey(ctx context.Context, q reviewerKeyQuery, keyID string, lock bool) (types.ReviewerKeyRecord, error) {
	var out types.ReviewerKeyRecord
	var registered, revoked *time.Time
	statement := `SELECT k.key_id,k.reviewer_authority,k.reviewer_id,k.data_kind,k.public_key_ed25519_hex,
 k.registered_at,r.revoked_at,COALESCE(r.event_id,''),
 COALESCE(o.event_id,'') FROM knowledge_reviewer_keys k
 LEFT JOIN knowledge_reviewer_key_revocations r ON r.key_id=k.key_id
 LEFT JOIN knowledge_outbox o ON o.aggregate_id='reviewer-key/'||k.key_id
 AND o.event_type='knowledge.reviewer.key.registered.v1'
 WHERE k.key_id=$1`
	if lock {
		statement += " FOR UPDATE OF k"
	}
	err := q.QueryRow(ctx, statement, keyID).Scan(&out.KeyId, &out.ReviewerAuthority,
		&out.ReviewerId, &out.DataKind, &out.PublicKeyEd25519Hex, &registered,
		&revoked, &out.RevocationEventId, &out.RegistrationEventId)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if registered == nil || out.RegistrationEventId == "" {
		return out, ErrArtifactUnavailable
	}
	out.SchemaVersion, out.RegisteredAt = "sea.rtw.reviewer-key.v1", registered.UTC().Format(time.RFC3339Nano)
	out.Status, out.RegistryRevision = "active", 1
	if revoked != nil {
		out.RevokedAt, out.Status, out.RegistryRevision = revoked.UTC().Format(time.RFC3339Nano), "revoked", 2
	}
	return out, nil
}

func (s *Store) GetReviewerKey(ctx context.Context, keyID string) (types.ReviewerKeyRecord, error) {
	return observe(ctx, s, "knowledge.reviewer.key.get", "reviewer-key:"+keyID,
		types.ReviewerKeyPath{KeyId: keyID}, func(ctx context.Context) (types.ReviewerKeyRecord, error) {
			if !citationIdentity(keyID) || len(keyID) > 128 {
				return types.ReviewerKeyRecord{}, invalid("bounded reviewer key identity required")
			}
			return reviewerKey(ctx, s.DB, keyID, false)
		})
}
