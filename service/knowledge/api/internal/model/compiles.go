package model

import (
	"context"
	"errors"
	"reflect"
	"sea-try-go/service/knowledge/api/internal/telemetry"
	"sort"
	"strings"
	"time"

	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateCompile(ctx context.Context, actor string, req types.CreateCompileReq) (types.Compile, error) {
	return observe(ctx, s, "knowledge.compile.create", operationID("compile/create/"+req.ModuleId+"/"+actor, req.IdempotencyKey), req, func(ctx context.Context) (types.Compile, error) {
		if req.PageId == "" || strings.TrimSpace(req.Guidance) == "" || len(req.SourceRevisionIds) == 0 {
			return types.Compile{}, invalid("compile page, guidance and sources required")
		}
		sort.Strings(req.SourceRevisionIds)
		return command(ctx, s, "compile/create/"+req.ModuleId+"/"+actor, req.IdempotencyKey, req, func(tx pgx.Tx) (types.Compile, error) {
			m, err := module(ctx, tx, req.ModuleId, true)
			if err != nil {
				return types.Compile{}, err
			}
			if err = enabled(m); err != nil {
				return types.Compile{}, err
			}
			var head string
			err = tx.QueryRow(ctx, "SELECT revision_id FROM knowledge_heads WHERE module_id=$1 AND kind='wiki' AND entity_id=$2", m.Id, req.PageId).Scan(&head)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return types.Compile{}, err
			}
			if head != req.BaseRevisionId {
				return types.Compile{}, conflict("compile base revision changed")
			}
			if err = s.validateRevisions(ctx, tx, ReleaseManifest{ModuleID: m.Id, SourceRevisionIDs: req.SourceRevisionIds}); err != nil {
				return types.Compile{}, err
			}
			var generation int64
			if err = tx.QueryRow(ctx, "SELECT COALESCE(MAX(generation),0)+1 FROM knowledge_compiles WHERE module_id=$1 AND page_id=$2", m.Id, req.PageId).Scan(&generation); err != nil {
				return types.Compile{}, err
			}
			if _, err = tx.Exec(ctx, "UPDATE knowledge_compiles SET data=jsonb_set(data,'{state}','\"SUPERSEDED\"'::jsonb) WHERE module_id=$1 AND page_id=$2 AND data->>'state'='BUILDING'", m.Id, req.PageId); err != nil {
				return types.Compile{}, err
			}
			h, err := hashInput(struct {
				ModuleID string   `json:"module_id"`
				PageID   string   `json:"page_id"`
				Base     string   `json:"base_revision_id"`
				Sources  []string `json:"source_revision_ids"`
				Guidance string   `json:"guidance"`
			}{m.Id, req.PageId, req.BaseRevisionId, req.SourceRevisionIds, req.Guidance})
			if err != nil {
				return types.Compile{}, err
			}
			c := types.Compile{CompileId: id("compile"), ModuleId: m.Id, PageId: req.PageId, BaseRevisionId: req.BaseRevisionId, SourceRevisionIds: req.SourceRevisionIds, Guidance: req.Guidance, InputHash: h, State: "BUILDING", Generation: generation}
			if err = saveJSON(ctx, tx, "INSERT INTO knowledge_compiles(id,module_id,page_id,generation,data) VALUES($1,$2,$3,$4,$5)", c, c.CompileId, c.ModuleId, c.PageId, c.Generation); err != nil {
				return c, err
			}
			return c, emit(ctx, tx, "knowledge.wiki.compile.requested.v1", m.Id, c)
		})

	})
}
func (s *Store) GetCompile(ctx context.Context, compileID string) (types.Compile, error) {
	return readJSON[types.Compile](ctx, s.DB, "SELECT data FROM knowledge_compiles WHERE id=$1", compileID)
}
func (s *Store) mutateCompile(ctx context.Context, compileID string, fn func(pgx.Tx, *types.Compile) error) (types.Compile, error) {
	initial, err := s.GetCompile(ctx, compileID)
	if err != nil {
		return initial, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return initial, err
	}
	defer tx.Rollback(context.Background())
	m, err := module(ctx, tx, initial.ModuleId, true)
	if err != nil {
		return initial, err
	}
	if err = enabled(m); err != nil {
		return initial, err
	}
	c, err := readJSON[types.Compile](ctx, tx, "SELECT data FROM knowledge_compiles WHERE id=$1 FOR UPDATE", compileID)
	if err != nil {
		return c, err
	}
	if err = setOperation(ctx, tx, "result:"+compileID); err != nil {
		return initial, err
	}
	actualFields(ctx, c)
	previous := c
	previousState := c.State
	if err = fn(tx, &c); err != nil {
		return c, err
	}
	liveLeaseRequired := previousState == "BUILDING" && (c.State == "ACCEPTED" || c.State == "FAILED")
	if err = saveExecutionResult(ctx, tx, "knowledge_compiles", c.CompileId, c, liveLeaseRequired); err != nil {
		return c, err
	}
	if reflect.DeepEqual(previous, c) {
		telemetry.Replay(ctx)
	}
	return c, tx.Commit(ctx)
}
func (s *Store) ClaimCompile(ctx context.Context, req types.ClaimCompileReq) (types.Compile, error) {
	return observe(ctx, s, "knowledge.compile.claim", "result:"+req.CompileId, req, func(ctx context.Context) (types.Compile, error) {
		if req.AttemptId == "" || req.LeaseEpoch <= 0 {
			return types.Compile{}, invalid("attempt and positive lease_epoch required")
		}
		return s.mutateCompile(ctx, req.CompileId, func(tx pgx.Tx, c *types.Compile) error {
			if c.State != "BUILDING" || c.Generation != req.Generation || c.CancelVersion != req.CancelVersion || c.InputHash != req.InputHash {
				return conflict("compile no longer accepts this generation")
			}
			if req.LeaseEpoch < c.LeaseEpoch || (req.LeaseEpoch == c.LeaseEpoch && req.AttemptId != c.AttemptId) {
				return conflictCode("FENCE_CONFLICT", "stale compile attempt")
			}
			if !leaseValid(req.LeaseExpiresAt) {
				return invalid("lease expiry must be a future RFC3339 timestamp")
			}
			c.LeaseExpiresAt = req.LeaseExpiresAt
			c.AttemptId = req.AttemptId
			c.LeaseEpoch = req.LeaseEpoch
			return nil
		})

	})
}
func (s *Store) AcceptCompile(ctx context.Context, req types.AcceptCompileReq) (types.Compile, error) {
	return observe(ctx, s, "knowledge.compile.accept", "result:"+req.CompileId, req, func(ctx context.Context) (types.Compile, error) {
		if req.State == "" {
			req.State = "READY"
		}
		if req.State != "READY" && req.State != "FAILED" {
			return types.Compile{}, invalid("compile result state must be READY or FAILED")
		}
		return s.mutateCompile(ctx, req.CompileId, func(tx pgx.Tx, c *types.Compile) error {
			if c.Generation != req.Generation || c.CancelVersion != req.CancelVersion || c.InputHash != req.InputHash || c.AttemptId == "" || c.AttemptId != req.AttemptId || c.LeaseEpoch != req.LeaseEpoch {
				return conflictCode("FENCE_CONFLICT", "compile result fence mismatch")
			}
			resultHash, err := hashInput(req)
			if err != nil {
				return err
			}
			if c.State == "ACCEPTED" || c.State == "FAILED" {
				if c.ResultHash == resultHash {
					return nil
				}
				return conflict("accepted compile result differs")
			}
			if c.State != "BUILDING" {
				return conflict("compile terminal or superseded")
			}
			if !leaseValid(c.LeaseExpiresAt) {
				return conflictCode("LEASE_EXPIRED", "compile lease expired")
			}
			if req.State == "FAILED" {
				if strings.TrimSpace(req.ErrorCode) == "" || req.ObjectKey != "" || req.ContentHash != "" || req.Title != "" || len(req.SourceRefs) != 0 {
					return invalid("FAILED compile requires error_code without result artifacts")
				}
				c.State = "FAILED"
				c.ErrorCode = req.ErrorCode
				c.ResultHash = resultHash
				return emit(ctx, tx, "knowledge.wiki.compile.failed.v1", c.ModuleId, *c)
			}
			if req.ErrorCode != "" {
				return invalid("READY compile cannot carry error_code")
			}
			if err = s.validateRevisions(ctx, tx, ReleaseManifest{ModuleID: c.ModuleId, SourceRevisionIDs: c.SourceRevisionIds}); err != nil {
				return err
			}
			allowed := map[string]bool{}
			for _, id := range c.SourceRevisionIds {
				allowed[id] = true
			}
			for _, ref := range req.SourceRefs {
				if !allowed[ref.RevisionId] {
					return invalid("compile reference outside frozen input")
				}
			}
			content, err := s.Objects.Get(ctx, req.ObjectKey, req.ContentHash)
			if err != nil {
				return invalid("compile object unreadable or hash mismatch")
			}
			in := revisionInput{ModuleID: c.ModuleId, EntityID: c.PageId, Kind: "wiki", BaseRevisionID: c.BaseRevisionId, Title: req.Title, Content: string(content), MediaType: "text/markdown", SourceRefs: req.SourceRefs, Actor: "btw.compile/" + c.CompileId}
			if err = validText(in); err != nil {
				return err
			}
			r, err := s.insertRevision(ctx, tx, in)
			if err != nil {
				return err
			}
			c.RevisionId = r.RevisionId
			c.ResultHash = resultHash
			c.State = "ACCEPTED"
			return emit(ctx, tx, "knowledge.wiki.compile.accepted.v1", c.ModuleId, *c)
		})

	})
}
func (s *Store) CancelCompile(ctx context.Context, actor string, req types.CancelCompileReq) (types.Compile, error) {
	return observe(ctx, s, "knowledge.compile.cancel", operationID("compile/cancel/"+req.CompileId+"/"+actor, req.IdempotencyKey), req, func(ctx context.Context) (types.Compile, error) {
		if strings.TrimSpace(req.Reason) == "" {
			return types.Compile{}, invalid("cancellation reason required")
		}
		initial, err := s.GetCompile(ctx, req.CompileId)
		if err != nil {
			return initial, err
		}
		return command(ctx, s, "compile/cancel/"+req.CompileId+"/"+actor, req.IdempotencyKey, req, func(tx pgx.Tx) (types.Compile, error) {
			if _, err = module(ctx, tx, initial.ModuleId, true); err != nil {
				return initial, err
			}
			c, err := readJSON[types.Compile](ctx, tx, "SELECT data FROM knowledge_compiles WHERE id=$1 FOR UPDATE", req.CompileId)
			if err != nil {
				return c, err
			}
			if c.State != "BUILDING" {
				return c, conflict("only unfinished compile can be cancelled")
			}
			c.State = "CANCELLED"
			c.CancelVersion++
			if err = saveJSON(ctx, tx, "UPDATE knowledge_compiles SET data=$2 WHERE id=$1", c, c.CompileId); err != nil {
				return c, err
			}
			return c, emit(ctx, tx, "knowledge.wiki.compile.cancelled.v1", c.ModuleId, struct {
				Compile types.Compile `json:"compile"`
				Reason  string        `json:"reason"`
			}{c, req.Reason})
		})

	})
}

func leaseValid(expiry string) bool {
	deadline, err := time.Parse(time.RFC3339Nano, expiry)
	return err == nil && time.Now().Before(deadline)
}
