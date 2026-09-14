package model

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

// historicalRelease checks one current database snapshot. Publication history,
// not the active pointer or merely a READY build, grants historical membership.
// A withdrawn member invalidates the whole release just as it does Published.
func (s *Store) historicalRelease(ctx context.Context, moduleID, releaseID string) (types.Release, error) {
	var raw []byte
	var lifecycle string
	var revisionsValid, buildValid bool
	err := s.DB.QueryRow(ctx, `SELECT r.data,m.data->>'lifecycle',
 NOT EXISTS (
   SELECT 1 FROM (
     SELECT jsonb_array_elements_text(r.data->'source_revision_ids') AS id,'source' AS kind
     UNION ALL
     SELECT jsonb_array_elements_text(r.data->'wiki_revision_ids') AS id,'wiki' AS kind
   ) member
   LEFT JOIN knowledge_revisions v ON v.id=member.id AND v.module_id=r.module_id
   WHERE v.id IS NULL OR v.withdrawn OR v.kind<>member.kind
 ),
 EXISTS (
   SELECT 1 FROM knowledge_publications p JOIN knowledge_builds b ON b.id=p.build_id
   WHERE p.module_id=r.module_id AND p.release_id=r.id
     AND b.module_id=r.module_id AND b.release_id=r.id AND b.data->>'state'='READY'
     AND b.data->>'manifest_hash'=r.data->>'manifest_hash'
 )
FROM knowledge_releases r JOIN knowledge_modules m ON m.id=r.module_id
WHERE r.module_id=$1 AND r.id=$2 AND EXISTS (
 SELECT 1 FROM knowledge_publications p WHERE p.module_id=r.module_id AND p.release_id=r.id
)`, moduleID, releaseID).Scan(&raw, &lifecycle, &revisionsValid, &buildValid)
	if errors.Is(err, pgx.ErrNoRows) {
		return types.Release{}, ErrNotFound
	}
	if err != nil {
		return types.Release{}, err
	}
	if lifecycle != "ENABLED" || !revisionsValid || !buildValid {
		return types.Release{}, ErrUnavailable
	}
	var r types.Release
	err = json.Unmarshal(raw, &r)
	return r, err
}

// GetHistoricalRelease resolves a fixed, ever-published release. Reads remain
// tied to this immutable release when an administrator publishes or rolls back.
func (s *Store) GetHistoricalRelease(ctx context.Context, moduleID, releaseID string) (types.Release, error) {
	r, err := s.historicalRelease(ctx, moduleID, releaseID)
	if err != nil {
		return types.Release{}, err
	}
	if _, err = s.readArtifact(ctx, r.ManifestRef, r.ManifestHash); err != nil {
		return types.Release{}, err
	}
	return s.historicalRelease(ctx, moduleID, releaseID)
}

func (s *Store) ListPublishedRevisions(ctx context.Context, req types.PublishedRevisionsReq) (types.ListRevisionsResp, error) {
	r, err := s.GetHistoricalRelease(ctx, req.ModuleId, req.ReleaseId)
	if err != nil {
		return types.ListRevisionsResp{}, err
	}
	ids := append(append([]string{}, r.SourceRevisionIds...), r.WikiRevisionIds...)
	page, err := listRecords[types.Revision](ctx, s, "revisions", req.ModuleId, req.ReleaseId, ids, req.Limit, req.Cursor)
	if err != nil {
		return types.ListRevisionsResp{}, err
	}
	if _, err = s.historicalRelease(ctx, req.ModuleId, req.ReleaseId); err != nil {
		return types.ListRevisionsResp{}, err
	}
	return types.ListRevisionsResp{Items: page.Items, NextCursor: page.NextCursor}, nil
}

func (s *Store) GetPublishedRevision(ctx context.Context, moduleID, releaseID, revisionID string) (types.Revision, error) {
	r, err := s.GetHistoricalRelease(ctx, moduleID, releaseID)
	if err != nil {
		return types.Revision{}, err
	}
	if !slices.Contains(r.SourceRevisionIds, revisionID) && !slices.Contains(r.WikiRevisionIds, revisionID) {
		return types.Revision{}, ErrNotFound
	}
	rev, err := s.GetModuleRevision(ctx, moduleID, revisionID)
	if err != nil {
		return types.Revision{}, err
	}
	// The release can become invalid while the object's bytes are in flight,
	// including withdrawal of another source on which this revision depends.
	if _, err = s.historicalRelease(ctx, moduleID, releaseID); err != nil {
		return types.Revision{}, err
	}
	return rev, nil
}
