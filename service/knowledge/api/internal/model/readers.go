package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

// ErrArtifactUnavailable distinguishes unreadable/corrupt bytes from withdrawn
// content. Clients may retry a 503; withdrawal is the domain's permanent 410.
var ErrArtifactUnavailable = errors.New("knowledge content temporarily unavailable")

func (s *Store) ListRevisions(ctx context.Context, req types.ModulePageReq) (types.ListRevisionsResp, error) {
	page, err := listRecords[types.Revision](ctx, s, "revisions", req.ModuleId, "", nil, req.Limit, req.Cursor)
	return types.ListRevisionsResp{Items: page.Items, NextCursor: page.NextCursor}, err
}

func (s *Store) ListReleases(ctx context.Context, req types.ModulePageReq) (types.ListReleasesResp, error) {
	page, err := listRecords[types.Release](ctx, s, "releases", req.ModuleId, "", nil, req.Limit, req.Cursor)
	return types.ListReleasesResp{Items: page.Items, NextCursor: page.NextCursor}, err
}

func (s *Store) ListBuilds(ctx context.Context, req types.ModulePageReq) (types.ListBuildsResp, error) {
	page, err := listRecords[types.Build](ctx, s, "builds", req.ModuleId, "", nil, req.Limit, req.Cursor)
	return types.ListBuildsResp{Items: page.Items, NextCursor: page.NextCursor}, err
}

func (s *Store) ListCompiles(ctx context.Context, req types.ModulePageReq) (types.ListCompilesResp, error) {
	page, err := listRecords[types.Compile](ctx, s, "compiles", req.ModuleId, "", nil, req.Limit, req.Cursor)
	return types.ListCompilesResp{Items: page.Items, NextCursor: page.NextCursor}, err
}

func (s *Store) GetModuleRelease(ctx context.Context, moduleID, releaseID string) (types.Release, error) {
	return readJSON[types.Release](ctx, s.DB, "SELECT data FROM knowledge_releases WHERE module_id=$1 AND id=$2", moduleID, releaseID)
}

func (s *Store) GetModuleBuild(ctx context.Context, moduleID, buildID string) (types.Build, error) {
	return readJSON[types.Build](ctx, s.DB, "SELECT data FROM knowledge_builds WHERE module_id=$1 AND id=$2", moduleID, buildID)
}

func (s *Store) GetModuleCompile(ctx context.Context, moduleID, compileID string) (types.Compile, error) {
	return readJSON[types.Compile](ctx, s.DB, "SELECT data FROM knowledge_compiles WHERE module_id=$1 AND id=$2", moduleID, compileID)
}

func (s *Store) moduleRevision(ctx context.Context, moduleID, revisionID string) (types.Revision, error) {
	var raw []byte
	var withdrawn bool
	var lifecycle string
	err := s.DB.QueryRow(ctx, `SELECT r.data,r.withdrawn,m.data->>'lifecycle'
FROM knowledge_revisions r JOIN knowledge_modules m ON m.id=r.module_id
WHERE r.module_id=$1 AND r.id=$2`, moduleID, revisionID).Scan(&raw, &withdrawn, &lifecycle)
	if errors.Is(err, pgx.ErrNoRows) {
		return types.Revision{}, ErrNotFound
	}
	if err != nil {
		return types.Revision{}, err
	}
	if withdrawn || lifecycle != "ENABLED" {
		return types.Revision{}, ErrUnavailable
	}
	var r types.Revision
	err = json.Unmarshal(raw, &r)
	return r, err
}

func (s *Store) readArtifact(ctx context.Context, key, hash string) ([]byte, error) {
	b, err := s.Objects.Get(ctx, key, hash)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %v", ErrArtifactUnavailable, err)
	}
	if len(b) > object.MaxObjectBytes || object.Hash(b) != hash {
		return nil, ErrArtifactUnavailable
	}
	return b, nil
}

// GetModuleRevision checks scope before fetching an object and rechecks current
// availability afterwards. External I/O holds no database transaction or lock.
func (s *Store) GetModuleRevision(ctx context.Context, moduleID, revisionID string) (types.Revision, error) {
	r, err := s.moduleRevision(ctx, moduleID, revisionID)
	if err != nil {
		return types.Revision{}, err
	}
	b, err := s.readArtifact(ctx, r.ObjectKey, r.ContentHash)
	if err != nil {
		return types.Revision{}, err
	}
	if _, err = s.moduleRevision(ctx, moduleID, revisionID); err != nil {
		return types.Revision{}, err
	}
	r.Content = string(b)
	return r, nil
}
