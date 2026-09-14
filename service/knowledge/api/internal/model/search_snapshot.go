package model

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

type currentSearchPublication struct {
	module  types.Module
	release types.Release
	build   types.Build
}

// currentSearchState reads the manually published pointer, its audit row, and
// its READY build in one PostgreSQL statement. A merely READY candidate cannot
// issue a search snapshot. The effective set is all members of this release;
// withdrawal invalidates the whole publication under the existing H03 rule.
func (s *Store) currentSearchState(ctx context.Context, moduleID string) (currentSearchPublication, error) {
	var state currentSearchPublication
	var moduleRaw, releaseRaw, buildRaw []byte
	var revisionsValid bool
	err := s.DB.QueryRow(ctx, `SELECT m.data,r.data,b.data,NOT EXISTS (
 SELECT 1 FROM (
  SELECT jsonb_array_elements_text(r.data->'source_revision_ids') AS id,'source' AS kind
  UNION ALL SELECT jsonb_array_elements_text(r.data->'wiki_revision_ids') AS id,'wiki' AS kind
 ) member LEFT JOIN knowledge_revisions v ON v.id=member.id AND v.module_id=r.module_id
 WHERE v.id IS NULL OR v.withdrawn OR v.kind<>member.kind
)
FROM knowledge_modules m
JOIN knowledge_publications p ON p.module_id=m.id
 AND p.pointer_revision=(m.data->>'pointer_revision')::bigint
 AND p.release_id=m.data->>'active_release_id'
 AND p.build_id=m.data->>'active_build_id'
JOIN knowledge_releases r ON r.id=p.release_id AND r.module_id=m.id
JOIN knowledge_builds b ON b.id=p.build_id AND b.release_id=r.id AND b.module_id=m.id
WHERE m.id=$1`, moduleID).Scan(&moduleRaw, &releaseRaw, &buildRaw, &revisionsValid)
	if errors.Is(err, pgx.ErrNoRows) {
		// Preserve the public distinction between a new/unpublished module and
		// a broken published pointer, without trusting client-supplied fields.
		m, lookupErr := module(ctx, s.DB, moduleID, false)
		if lookupErr != nil {
			return state, lookupErr
		}
		if m.Lifecycle != "ENABLED" {
			return state, ErrUnavailable
		}
		if m.ActiveReleaseId == "" && m.ActiveBuildId == "" && m.PointerRevision == 0 {
			return state, ErrNotFound
		}
		return state, ErrUnavailable
	}
	if err != nil {
		return state, err
	}
	if err = json.Unmarshal(moduleRaw, &state.module); err != nil {
		return state, err
	}
	if err = json.Unmarshal(releaseRaw, &state.release); err != nil {
		return state, err
	}
	if err = json.Unmarshal(buildRaw, &state.build); err != nil {
		return state, err
	}
	if state.module.Lifecycle != "ENABLED" || !revisionsValid ||
		state.module.PointerRevision < 1 || state.module.ActiveReleaseId != state.release.ReleaseId ||
		state.module.ActiveBuildId != state.build.BuildId ||
		state.release.ModuleId != moduleID || state.build.ModuleId != moduleID ||
		state.build.ReleaseId != state.release.ReleaseId || state.build.State != "READY" ||
		state.build.Generation < 1 || state.build.ManifestHash != state.release.ManifestHash ||
		state.build.IndexManifestRef == "" || !citationHash(state.build.IndexManifestHash) {
		return currentSearchPublication{}, ErrUnavailable
	}
	return state, nil
}

// GetCurrentSearchSnapshot derives the effective three-lane search input from
// RTW's current manual publication. Historical citation reads intentionally use
// their fixed publication revision and do not call this moving-pointer reader.
func (s *Store) GetCurrentSearchSnapshot(ctx context.Context, moduleID string) (types.SearchSnapshot, error) {
	return observe(ctx, s, "knowledge.search.snapshot.current", "search-snapshot:"+moduleID,
		types.ModulePath{ModuleId: moduleID}, func(ctx context.Context) (types.SearchSnapshot, error) {
			var empty types.SearchSnapshot
			if !citationIdentity(moduleID) {
				return empty, invalid("module identity required")
			}
			initial, err := s.currentSearchState(ctx, moduleID)
			if err != nil {
				return empty, err
			}
			manifestRaw, err := s.readArtifact(ctx, initial.release.ManifestRef, initial.release.ManifestHash)
			if err != nil {
				return empty, err
			}
			var manifest ReleaseManifest
			if err = decodeCitationJSON(manifestRaw, &manifest); err != nil || !reflect.DeepEqual(manifest, manifestOf(initial.release)) {
				return empty, ErrArtifactUnavailable
			}
			if err = s.validateRevisions(ctx, s.DB, manifest); err != nil {
				return empty, err
			}
			if err = s.verifyIndex(ctx, initial.release, initial.build,
				initial.build.IndexManifestRef, initial.build.IndexManifestHash); err != nil {
				return empty, err
			}
			indexRaw, err := s.readArtifact(ctx, initial.build.IndexManifestRef, initial.build.IndexManifestHash)
			if err != nil {
				return empty, err
			}
			var index IndexManifest
			if err = decodeCitationJSON(indexRaw, &index); err != nil {
				return empty, ErrArtifactUnavailable
			}
			indexes := make(map[string]types.CitationObject, 3)
			for _, lane := range index.Lanes {
				if lane.Artifact.Key != object.Key(lane.Artifact.SHA256) ||
					!citationHash(lane.Artifact.SHA256) || indexes[lane.Profile.Lane].Key != "" {
					return empty, ErrArtifactUnavailable
				}
				indexes[lane.Profile.Lane] = types.CitationObject{Key: lane.Artifact.Key, Sha256: lane.Artifact.SHA256}
			}
			if len(indexes) != 3 || indexes["dense"].Key == "" || indexes["sparse"].Key == "" || indexes["multivector"].Key == "" {
				return empty, ErrArtifactUnavailable
			}
			// Object reads happen outside a database transaction. A fresh query
			// catches a concurrent rollback, new publication or withdrawal.
			current, err := s.currentSearchState(ctx, moduleID)
			if err != nil {
				return empty, err
			}
			if current.module.PointerRevision != initial.module.PointerRevision ||
				current.release.ReleaseId != initial.release.ReleaseId ||
				current.build.BuildId != initial.build.BuildId ||
				current.build.Generation != initial.build.Generation ||
				current.build.IndexManifestRef != initial.build.IndexManifestRef ||
				current.build.IndexManifestHash != initial.build.IndexManifestHash {
				return empty, conflict("current publication changed while reading indexes")
			}
			ids := append(append([]string{}, initial.release.SourceRevisionIds...), initial.release.WikiRevisionIds...)
			return types.SearchSnapshot{ModuleId: moduleID, ReleaseId: initial.release.ReleaseId,
				Generation: initial.build.Generation, PublicationRevision: strconv.FormatInt(initial.module.PointerRevision, 10),
				Indexes: indexes, ValidRevisionIds: ids}, nil
		})
}
