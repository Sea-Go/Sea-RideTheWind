package model

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

// ReleaseManifest is the immutable H03 r1 build input; hash is SHA256 of its exact JSON bytes.
type ReleaseManifest struct {
	SchemaVersion     int                      `json:"schema_version"`
	ModuleID          string                   `json:"module_id"`
	ReleaseID         string                   `json:"release_id"`
	SourceRevisionIDs []string                 `json:"source_revision_ids"`
	WikiRevisionIDs   []string                 `json:"wiki_revision_ids"`
	ChunkingProfile   string                   `json:"chunking_profile"`
	RetrievalProfiles []types.RetrievalProfile `json:"retrieval_profiles"`
}

func validateProfiles(profiles []types.RetrievalProfile) error {
	if len(profiles) != 3 {
		return invalid("dense, sparse and multivector profiles required")
	}
	seen := map[string]bool{}
	for _, p := range profiles {
		if seen[p.Lane] || p.Encoder == "" || p.Tokenizer == "" || p.Space == "" || p.Dimensions <= 0 {
			return invalid("invalid retrieval profile")
		}
		seen[p.Lane] = true
		switch p.Lane {
		case "dense", "sparse":
			if p.Mask != "" || p.Aggregation != "" {
				return invalid("token mask and aggregation belong to multivector")
			}
		case "multivector":
			if p.Mask == "" || p.Aggregation != "maxsim" {
				return invalid("multivector mask and maxsim aggregation required")
			}
		default:
			return invalid("unknown retrieval lane")
		}
	}
	return nil
}
func (s *Store) CreateRelease(ctx context.Context, actor string, req types.CreateReleaseReq) (types.Release, error) {
	if err := validateProfiles(req.RetrievalProfiles); err != nil {
		return types.Release{}, err
	}
	if len(req.SourceRevisionIds) == 0 || req.ChunkingProfile == "" {
		return types.Release{}, invalid("sources and chunking profile required")
	}
	sort.Strings(req.SourceRevisionIds)
	sort.Strings(req.WikiRevisionIds)
	sort.Slice(req.RetrievalProfiles, func(i, j int) bool { return req.RetrievalProfiles[i].Lane < req.RetrievalProfiles[j].Lane })
	return command(ctx, s, "release/"+req.ModuleId+"/"+actor, req.IdempotencyKey, req, func(tx pgx.Tx) (types.Release, error) {
		m, err := module(ctx, tx, req.ModuleId, true)
		if err != nil {
			return types.Release{}, err
		}
		if err = enabled(m); err != nil {
			return types.Release{}, err
		}
		releaseID := id("release")
		manifest := ReleaseManifest{1, m.Id, releaseID, req.SourceRevisionIds, req.WikiRevisionIds, req.ChunkingProfile, req.RetrievalProfiles}
		if manifest.WikiRevisionIDs == nil {
			manifest.WikiRevisionIDs = []string{}
		}
		if err = s.validateRevisions(ctx, tx, manifest); err != nil {
			return types.Release{}, err
		}
		raw, err := encode(manifest)
		if err != nil {
			return types.Release{}, err
		}
		ref, hash, err := s.Objects.Put(ctx, raw)
		if err != nil {
			return types.Release{}, err
		}
		var ordinal int64
		if err = tx.QueryRow(ctx, "SELECT COALESCE(MAX(ordinal),0)+1 FROM knowledge_releases WHERE module_id=$1", m.Id).Scan(&ordinal); err != nil {
			return types.Release{}, err
		}
		r := types.Release{ReleaseId: releaseID, ModuleId: m.Id, Ordinal: ordinal, SourceRevisionIds: manifest.SourceRevisionIDs, WikiRevisionIds: manifest.WikiRevisionIDs, ChunkingProfile: manifest.ChunkingProfile, RetrievalProfiles: manifest.RetrievalProfiles, ManifestHash: hash, ManifestRef: ref, CreatedAt: now()}
		if err = saveJSON(ctx, tx, "INSERT INTO knowledge_releases(id,module_id,ordinal,data) VALUES($1,$2,$3,$4)", r, r.ReleaseId, r.ModuleId, r.Ordinal); err != nil {
			return r, err
		}
		return r, emit(ctx, tx, "knowledge.release.frozen.v1", m.Id, r)
	})
}
func (s *Store) validateRevisions(ctx context.Context, q queryer, m ReleaseManifest) error {
	seen := map[string]bool{}
	sources := map[string]bool{}
	entities := map[string]bool{}
	for _, revID := range m.SourceRevisionIDs {
		sources[revID] = true
	}
	for _, kind := range []string{"source", "wiki"} {
		ids := m.SourceRevisionIDs
		if kind == "wiki" {
			ids = m.WikiRevisionIDs
		}
		for _, revID := range ids {
			if seen[revID] {
				return invalid("duplicate revision")
			}
			seen[revID] = true
			r, err := revision(ctx, q, revID)
			if err != nil {
				return err
			}
			if r.ModuleId != m.ModuleID || r.Kind != kind {
				return invalid("revision kind or module mismatch")
			}
			if r.Withdrawn {
				return ErrUnavailable
			}
			entityKey := kind + "/" + r.EntityId
			if entities[entityKey] {
				return invalid("multiple revisions of one entity")
			}
			entities[entityKey] = true
			if _, err = s.Objects.Get(ctx, r.ObjectKey, r.ContentHash); err != nil {
				return fmt.Errorf("read revision object: %w", ErrUnavailable)
			}
			for _, ref := range r.SourceRefs {
				if !sources[ref.RevisionId] {
					return invalid("wiki source missing from release")
				}
			}
		}
	}
	return nil
}
func (s *Store) GetRelease(ctx context.Context, releaseID string) (types.Release, error) {
	return readJSON[types.Release](ctx, s.DB, "SELECT data FROM knowledge_releases WHERE id=$1", releaseID)
}
func manifestOf(r types.Release) ReleaseManifest {
	return ReleaseManifest{1, r.ModuleId, r.ReleaseId, r.SourceRevisionIds, r.WikiRevisionIds, r.ChunkingProfile, r.RetrievalProfiles}
}
func (s *Store) published(ctx context.Context, q queryer, m types.Module) (types.Release, error) {
	if err := enabled(m); err != nil {
		return types.Release{}, err
	}
	if m.ActiveReleaseId == "" {
		return types.Release{}, ErrNotFound
	}
	r, err := readJSON[types.Release](ctx, q, "SELECT data FROM knowledge_releases WHERE id=$1", m.ActiveReleaseId)
	if err != nil {
		return r, err
	}
	if err = s.validateRevisions(ctx, q, manifestOf(r)); err != nil {
		return r, err
	}
	if _, err = s.Objects.Get(ctx, r.ManifestRef, r.ManifestHash); err != nil {
		return r, ErrUnavailable
	}
	b, err := readJSON[types.Build](ctx, q, "SELECT data FROM knowledge_builds WHERE id=$1", m.ActiveBuildId)
	if err != nil {
		return r, err
	}
	if b.State != "READY" || b.ReleaseId != r.ReleaseId {
		return r, ErrUnavailable
	}
	return r, nil
}
func (s *Store) Published(ctx context.Context, moduleID string) (types.Release, error) {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return types.Release{}, err
	}
	defer tx.Rollback(context.Background())
	m, err := module(ctx, tx, moduleID, false)
	if err != nil {
		return types.Release{}, err
	}
	r, err := s.published(ctx, tx, m)
	if err != nil {
		return r, err
	}
	return r, tx.Commit(ctx)
}
func (s *Store) Activate(ctx context.Context, actor string, req types.ActivateReq) (types.ReleaseState, error) {
	if req.ExpectedPointerRevision < 0 || strings.TrimSpace(req.Reason) == "" {
		return types.ReleaseState{}, invalid("expected pointer revision and reason required")
	}
	key := req.IdempotencyKey
	// Existing H02 clients do not send a key. The compare-and-swap precondition is their stable operation identity.
	if key == "" {
		key = fmt.Sprintf("pointer:%d", req.ExpectedPointerRevision)
	}
	return command(ctx, s, "activation/"+req.ModuleId+"/"+actor, key, req, func(tx pgx.Tx) (types.ReleaseState, error) {
		m, err := module(ctx, tx, req.ModuleId, true)
		if err != nil {
			return types.ReleaseState{}, err
		}
		if err = enabled(m); err != nil {
			return types.ReleaseState{}, err
		}
		if m.PointerRevision != req.ExpectedPointerRevision {
			return types.ReleaseState{}, conflict("pointer revision changed")
		}
		r, err := readJSON[types.Release](ctx, tx, "SELECT data FROM knowledge_releases WHERE id=$1", req.ReleaseId)
		if err != nil {
			return types.ReleaseState{}, err
		}
		b, err := readJSON[types.Build](ctx, tx, "SELECT data FROM knowledge_builds WHERE id=$1", req.BuildId)
		if err != nil {
			return types.ReleaseState{}, err
		}
		if r.ModuleId != m.Id || b.ReleaseId != r.ReleaseId || b.ManifestHash != r.ManifestHash || b.State != "READY" {
			return types.ReleaseState{}, conflict("release and READY build must match")
		}
		if err = s.validateRevisions(ctx, tx, manifestOf(r)); err != nil {
			return types.ReleaseState{}, err
		}
		if _, err = s.Objects.Get(ctx, r.ManifestRef, r.ManifestHash); err != nil {
			return types.ReleaseState{}, ErrUnavailable
		}
		if err = s.verifyIndex(ctx, r, b, b.IndexManifestRef, b.IndexManifestHash); err != nil {
			return types.ReleaseState{}, err
		}
		m.ActiveReleaseId = r.ReleaseId
		m.ActiveBuildId = b.BuildId
		m.PointerRevision++
		m.Release = fmt.Sprintf("v%d", r.Ordinal)
		m.Sources = len(r.SourceRevisionIds)
		m.Pages = len(r.WikiRevisionIds)
		m.Updated = now()
		if err = saveJSON(ctx, tx, "UPDATE knowledge_modules SET data=$2 WHERE id=$1", m, m.Id); err != nil {
			return types.ReleaseState{}, err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO knowledge_publications(module_id,pointer_revision,release_id,build_id,actor,reason) VALUES($1,$2,$3,$4,$5,$6)", m.Id, m.PointerRevision, r.ReleaseId, b.BuildId, actor, req.Reason); err != nil {
			return types.ReleaseState{}, err
		}
		state := types.ReleaseState{ActiveReleaseId: r.ReleaseId, ActiveBuildId: b.BuildId, PointerRevision: m.PointerRevision, CandidateReleaseId: r.ReleaseId, BuildId: b.BuildId, BuildState: b.State, ManifestHash: r.ManifestHash}
		return state, emit(ctx, tx, "knowledge.release.activated.v1", m.Id, state)
	})
}
func (s *Store) Current(ctx context.Context, moduleID string) (types.ReleaseState, error) {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return types.ReleaseState{}, err
	}
	defer tx.Rollback(context.Background())
	m, err := module(ctx, tx, moduleID, false)
	if err != nil {
		return types.ReleaseState{}, err
	}
	out := types.ReleaseState{ActiveReleaseId: m.ActiveReleaseId, ActiveBuildId: m.ActiveBuildId, PointerRevision: m.PointerRevision, BuildState: "NOT_BUILT"}
	r, err := readJSON[types.Release](ctx, tx, "SELECT data FROM knowledge_releases WHERE module_id=$1 ORDER BY ordinal DESC LIMIT 1", m.Id)
	if err == ErrNotFound {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.CandidateReleaseId = r.ReleaseId
	out.ManifestHash = r.ManifestHash
	b, err := readJSON[types.Build](ctx, tx, "SELECT data FROM knowledge_builds WHERE release_id=$1 ORDER BY generation DESC LIMIT 1", r.ReleaseId)
	if err == ErrNotFound {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.BuildId = b.BuildId
	out.BuildState = b.State
	return out, tx.Commit(ctx)
}
