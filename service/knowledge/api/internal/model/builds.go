package model

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sea-try-go/service/knowledge/api/internal/telemetry"
	"strings"

	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

// ArtifactRef identifies exact immutable bytes in the configured object backend.
type ArtifactRef struct {
	Key    string `json:"key"`
	SHA256 string `json:"sha256"`
}
type LaneManifest struct {
	Profile     types.RetrievalProfile `json:"profile"`
	Artifact    ArtifactRef            `json:"artifact"`
	ChunkCount  int64                  `json:"chunk_count"`
	Shards      int                    `json:"shards"`
	ProbePassed bool                   `json:"probe_passed"`
	Probe       ArtifactRef            `json:"probe"`
}

// IndexManifest is the H06 r1 acceptance fixture. It proves structural readiness only;
// real indexing and independent retrieval probes remain the producer's responsibility.
type IndexManifest struct {
	SchemaVersion     int            `json:"schema_version"`
	BuildID           string         `json:"build_id"`
	ReleaseID         string         `json:"release_id"`
	Generation        int64          `json:"generation"`
	InputManifestHash string         `json:"input_manifest_hash"`
	ChunkManifest     ArtifactRef    `json:"chunk_manifest"`
	ChunkCount        int64          `json:"chunk_count"`
	Lanes             []LaneManifest `json:"lanes"`
}

func (s *Store) verifyIndex(ctx context.Context, r types.Release, b types.Build, ref, hash string) error {
	raw, err := s.Objects.Get(ctx, ref, hash)
	if err != nil {
		return invalid("index manifest unreadable or hash mismatch")
	}
	var m IndexManifest
	if err = json.Unmarshal(raw, &m); err != nil {
		return invalid("index manifest JSON invalid")
	}
	if m.SchemaVersion != 1 || m.BuildID != b.BuildId || m.ReleaseID != r.ReleaseId || m.Generation != b.Generation || m.InputManifestHash != r.ManifestHash {
		return conflict("index manifest does not match frozen build")
	}
	if m.ChunkCount <= 0 || len(m.Lanes) != 3 {
		return invalid("nonempty chunks and three lanes required")
	}
	if _, err = s.Objects.Get(ctx, m.ChunkManifest.Key, m.ChunkManifest.SHA256); err != nil {
		return invalid("chunk manifest unreadable or hash mismatch")
	}
	profiles := map[string]types.RetrievalProfile{}
	for _, p := range r.RetrievalProfiles {
		profiles[p.Lane] = p
	}
	seen := map[string]bool{}
	for _, lane := range m.Lanes {
		expected, ok := profiles[lane.Profile.Lane]
		if !ok || seen[lane.Profile.Lane] || !reflect.DeepEqual(expected, lane.Profile) {
			return invalid("missing, duplicate or incompatible lane")
		}
		seen[lane.Profile.Lane] = true
		if lane.ChunkCount != m.ChunkCount || lane.Shards < 1 || !lane.ProbePassed {
			return invalid("lane counts, shards or probe incomplete")
		}
		for _, a := range []ArtifactRef{lane.Artifact, lane.Probe} {
			if _, err = s.Objects.Get(ctx, a.Key, a.SHA256); err != nil {
				return invalid("lane artifact or probe unreadable or hash mismatch")
			}
		}
	}
	return nil
}
func (s *Store) CreateBuild(ctx context.Context, actor string, req types.CreateBuildReq) (types.Build, error) {
	return observe(ctx, s, "knowledge.build.create", operationID("build/create/"+req.ReleaseId+"/"+actor, req.IdempotencyKey), req, func(ctx context.Context) (types.Build, error) {
		r, err := s.GetRelease(ctx, req.ReleaseId)
		if err != nil {
			return types.Build{}, err
		}
		return command(ctx, s, "build/create/"+r.ReleaseId+"/"+actor, req.IdempotencyKey, req, func(tx pgx.Tx) (types.Build, error) {
			m, err := module(ctx, tx, r.ModuleId, true)
			if err != nil {
				return types.Build{}, err
			}
			if err = enabled(m); err != nil {
				return types.Build{}, err
			}
			if err = s.validateRevisions(ctx, tx, manifestOf(r)); err != nil {
				return types.Build{}, err
			}
			var generation int64
			if err = tx.QueryRow(ctx, "SELECT COALESCE(MAX(generation),0)+1 FROM knowledge_builds WHERE release_id=$1", r.ReleaseId).Scan(&generation); err != nil {
				return types.Build{}, err
			}
			// READY historical builds remain eligible for rollback; only unfinished generations are superseded.
			if _, err = tx.Exec(ctx, "UPDATE knowledge_builds SET data=jsonb_set(data,'{state}','\"SUPERSEDED\"'::jsonb) WHERE release_id=$1 AND data->>'state'='BUILDING'", r.ReleaseId); err != nil {
				return types.Build{}, err
			}
			b := types.Build{BuildId: id("build"), ReleaseId: r.ReleaseId, ModuleId: r.ModuleId, ManifestHash: r.ManifestHash, Generation: generation, State: "BUILDING"}
			if err = saveJSON(ctx, tx, "INSERT INTO knowledge_builds(id,release_id,module_id,generation,data) VALUES($1,$2,$3,$4,$5)", b, b.BuildId, b.ReleaseId, b.ModuleId, b.Generation); err != nil {
				return b, err
			}
			payload := struct {
				Build       types.Build `json:"build"`
				ManifestRef string      `json:"manifest_ref"`
			}{b, r.ManifestRef}
			return b, emit(ctx, tx, "knowledge.index.build.requested.v1", r.ModuleId, payload)
		})

	})
}
func (s *Store) GetBuild(ctx context.Context, buildID string) (types.Build, error) {
	return readJSON[types.Build](ctx, s.DB, "SELECT data FROM knowledge_builds WHERE id=$1", buildID)
}
func (s *Store) mutateBuild(ctx context.Context, buildID string, fn func(pgx.Tx, *types.Build) error) (types.Build, error) {
	initial, err := s.GetBuild(ctx, buildID)
	if err != nil {
		return initial, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return types.Build{}, err
	}
	defer tx.Rollback(context.Background())
	m, err := module(ctx, tx, initial.ModuleId, true)
	if err != nil {
		return initial, err
	}
	if err = enabled(m); err != nil {
		return initial, err
	}
	b, err := readJSON[types.Build](ctx, tx, "SELECT data FROM knowledge_builds WHERE id=$1 FOR UPDATE", buildID)
	if err != nil {
		return b, err
	}
	if err = setOperation(ctx, tx, "result:"+buildID); err != nil {
		return initial, err
	}
	actualFields(ctx, b)
	previous := b
	previousState := b.State
	if err = fn(tx, &b); err != nil {
		return b, err
	}
	liveLeaseRequired := previousState == "BUILDING" && (b.State == "READY" || b.State == "FAILED")
	if err = saveExecutionResult(ctx, tx, "knowledge_builds", b.BuildId, b, liveLeaseRequired); err != nil {
		return b, err
	}
	if reflect.DeepEqual(previous, b) {
		telemetry.Replay(ctx)
	}
	return b, tx.Commit(ctx)
}
func sameFence(b types.Build, generation, cancel int64, hash string) bool {
	return b.Generation == generation && b.CancelVersion == cancel && b.ManifestHash == hash
}
func (s *Store) ClaimBuild(ctx context.Context, req types.ClaimBuildReq) (types.Build, error) {
	return observe(ctx, s, "knowledge.build.claim", "result:"+req.BuildId, req, func(ctx context.Context) (types.Build, error) {
		if req.AttemptId == "" || req.LeaseEpoch <= 0 {
			return types.Build{}, invalid("attempt and positive lease_epoch required")
		}
		return s.mutateBuild(ctx, req.BuildId, func(tx pgx.Tx, b *types.Build) error {
			if b.State != "BUILDING" || !sameFence(*b, req.Generation, req.CancelVersion, req.ManifestHash) {
				return conflict("build is terminal, superseded or cancelled")
			}
			if req.LeaseEpoch < b.LeaseEpoch || (req.LeaseEpoch == b.LeaseEpoch && req.AttemptId != b.AttemptId) {
				return conflictCode("FENCE_CONFLICT", "stale attempt lease")
			}
			if !leaseValid(req.LeaseExpiresAt) {
				return invalid("lease expiry must be a future RFC3339 timestamp")
			}
			b.LeaseExpiresAt = req.LeaseExpiresAt
			b.AttemptId = req.AttemptId
			b.LeaseEpoch = req.LeaseEpoch
			return nil
		})

	})
}
func (s *Store) AcceptBuild(ctx context.Context, req types.AcceptBuildReq) (types.Build, error) {
	return observe(ctx, s, "knowledge.build.accept", "result:"+req.BuildId, req, func(ctx context.Context) (types.Build, error) {
		if req.State != "READY" && req.State != "FAILED" {
			return types.Build{}, invalid("result state must be READY or FAILED")
		}
		return s.mutateBuild(ctx, req.BuildId, func(tx pgx.Tx, b *types.Build) error {
			if !sameFence(*b, req.Generation, req.CancelVersion, req.ManifestHash) || b.AttemptId == "" || b.AttemptId != req.AttemptId || b.LeaseEpoch != req.LeaseEpoch {
				return conflictCode("FENCE_CONFLICT", "result attempt, generation, cancellation or input mismatch")
			}
			if b.State == req.State {
				if b.IndexManifestRef == req.IndexManifestRef && b.IndexManifestHash == req.IndexManifestHash && b.ErrorCode == req.ErrorCode {
					return nil
				}
				return conflict("terminal result payload differs")
			}
			if b.State != "BUILDING" {
				return conflict("build no longer accepts results")
			}
			if !leaseValid(b.LeaseExpiresAt) {
				return conflictCode("LEASE_EXPIRED", "result lease expired")
			}
			if req.State == "READY" {
				if req.ErrorCode != "" {
					return invalid("READY cannot carry an error")
				}
				r, err := readJSON[types.Release](ctx, tx, "SELECT data FROM knowledge_releases WHERE id=$1", b.ReleaseId)
				if err != nil {
					return err
				}
				if err = s.validateRevisions(ctx, tx, manifestOf(r)); err != nil {
					return err
				}
				if err = s.verifyIndex(ctx, r, *b, req.IndexManifestRef, req.IndexManifestHash); err != nil {
					return err
				}
			} else if strings.TrimSpace(req.ErrorCode) == "" || req.IndexManifestHash != "" || req.IndexManifestRef != "" {
				return invalid("FAILED requires error_code and no READY manifest")
			}
			b.State = req.State
			b.IndexManifestRef = req.IndexManifestRef
			b.IndexManifestHash = req.IndexManifestHash
			b.ErrorCode = req.ErrorCode
			return emit(ctx, tx, "knowledge.index.build.accepted.v1", b.ModuleId, *b)
		})

	})
}
func (s *Store) CancelBuild(ctx context.Context, actor string, req types.CancelBuildReq) (types.Build, error) {
	return observe(ctx, s, "knowledge.build.cancel", operationID("build/cancel/"+req.BuildId+"/"+actor, req.IdempotencyKey), req, func(ctx context.Context) (types.Build, error) {
		if strings.TrimSpace(req.Reason) == "" {
			return types.Build{}, invalid("cancellation reason required")
		}
		initial, err := s.GetBuild(ctx, req.BuildId)
		if err != nil {
			return initial, err
		}
		return command(ctx, s, "build/cancel/"+req.BuildId+"/"+actor, req.IdempotencyKey, req, func(tx pgx.Tx) (types.Build, error) {
			if _, err := module(ctx, tx, initial.ModuleId, true); err != nil {
				return types.Build{}, err
			}
			b, err := readJSON[types.Build](ctx, tx, "SELECT data FROM knowledge_builds WHERE id=$1 FOR UPDATE", req.BuildId)
			if err != nil {
				return b, err
			}
			if b.State != "BUILDING" {
				return b, conflict("only unfinished builds can be cancelled")
			}
			b.State = "CANCELLED"
			b.CancelVersion++
			b.ErrorCode = "CANCELLED"
			if err = saveJSON(ctx, tx, "UPDATE knowledge_builds SET data=$2 WHERE id=$1", b, b.BuildId); err != nil {
				return b, err
			}
			return b, emit(ctx, tx, "knowledge.index.build.cancelled.v1", b.ModuleId, struct {
				Build  types.Build `json:"build"`
				Reason string      `json:"reason"`
			}{b, req.Reason})
		})

	})
}

func (s *Store) StatusSummary(ctx context.Context) (string, error) {
	var pending int64
	err := s.DB.QueryRow(ctx, "SELECT count(*) FROM knowledge_outbox WHERE delivered_at IS NULL").Scan(&pending)
	return fmt.Sprintf("outbox_pending=%d", pending), err
}
