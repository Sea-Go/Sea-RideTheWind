package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

type revisionInput struct {
	ModuleID, EntityID, Kind, BaseRevisionID, Title, Content, MediaType, Provenance, Actor string
	SourceRefs                                                                             []types.SourceRef
}

func (s *Store) CreateSource(ctx context.Context, actor string, req types.CreateSourceReq) (types.Revision, error) {
	return observe(ctx, s, "knowledge.source.create", operationID("revision/"+req.ModuleId+"/"+actor, req.IdempotencyKey), req, func(ctx context.Context) (types.Revision, error) {
		return s.createRevision(ctx, req.IdempotencyKey, revisionInput{ModuleID: req.ModuleId, EntityID: req.EntityId, Kind: "source", BaseRevisionID: req.BaseRevisionId, Title: req.Title, Content: req.Content, MediaType: req.MediaType, Provenance: req.Provenance, Actor: actor})

	})
}
func (s *Store) CreateWiki(ctx context.Context, actor string, req types.CreateWikiReq) (types.Revision, error) {
	return observe(ctx, s, "knowledge.wiki.create", operationID("revision/"+req.ModuleId+"/"+actor, req.IdempotencyKey), req, func(ctx context.Context) (types.Revision, error) {
		if req.PageId == "" {
			return types.Revision{}, invalid("page id required")
		}
		return s.createRevision(ctx, req.IdempotencyKey, revisionInput{ModuleID: req.ModuleId, EntityID: req.PageId, Kind: "wiki", BaseRevisionID: req.BaseRevisionId, Title: req.Title, Content: req.Content, MediaType: "text/markdown", SourceRefs: req.SourceRefs, Actor: actor})

	})
}
func validText(in revisionInput) error {
	if !utf8.ValidString(in.Content) || strings.TrimSpace(in.Content) == "" || strings.TrimSpace(in.Title) == "" {
		return invalid("nonempty UTF-8 content and title required")
	}
	if in.MediaType != "text/markdown" && in.MediaType != "text/plain" {
		return invalid("only text/markdown and text/plain are implemented")
	}
	if in.Kind == "source" && strings.TrimSpace(in.Provenance) == "" {
		return invalid("source provenance required")
	}
	if in.Kind == "wiki" && len(in.SourceRefs) == 0 {
		return invalid("wiki source references required")
	}
	return nil
}
func (s *Store) createRevision(ctx context.Context, key string, in revisionInput) (types.Revision, error) {
	if err := validText(in); err != nil {
		return types.Revision{}, err
	}
	return command(ctx, s, "revision/"+in.ModuleID+"/"+in.Actor, key, in, func(tx pgx.Tx) (types.Revision, error) {
		m, err := module(ctx, tx, in.ModuleID, true)
		if err != nil {
			return types.Revision{}, err
		}
		if err = enabled(m); err != nil {
			return types.Revision{}, err
		}
		return s.insertRevision(ctx, tx, in)
	})
}
func (s *Store) insertRevision(ctx context.Context, tx pgx.Tx, in revisionInput) (types.Revision, error) {
	if in.EntityID == "" {
		if in.BaseRevisionID != "" {
			return types.Revision{}, invalid("source_id required for a revision")
		}
		in.EntityID = id(in.Kind)
	}
	var head string
	err := tx.QueryRow(ctx, "SELECT revision_id FROM knowledge_heads WHERE module_id=$1 AND kind=$2 AND entity_id=$3", in.ModuleID, in.Kind, in.EntityID).Scan(&head)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return types.Revision{}, err
	}
	if head != in.BaseRevisionID {
		return types.Revision{}, conflict("base revision no longer current")
	}
	for _, ref := range in.SourceRefs {
		r, err := revision(ctx, tx, ref.RevisionId)
		if err != nil {
			return types.Revision{}, err
		}
		if r.Withdrawn || r.ModuleId != in.ModuleID || r.Kind != "source" {
			return types.Revision{}, invalid("wiki references must target available sources in its module")
		}
		sourceBytes, readErr := s.Objects.Get(ctx, r.ObjectKey, r.ContentHash)
		if readErr != nil {
			return types.Revision{}, ErrUnavailable
		}
		r.Content = string(sourceBytes)
		if !validLocator(r, ref.Locator) {
			return types.Revision{}, invalid("source locator must use paragraph:N within source")
		}
	}
	objectKey, hash, err := s.Objects.Put(ctx, []byte(in.Content))
	if err != nil {
		return types.Revision{}, err
	}
	refs := in.SourceRefs
	if refs == nil {
		refs = []types.SourceRef{}
	}
	r := types.Revision{RevisionId: id("revision"), ModuleId: in.ModuleID, EntityId: in.EntityID, Kind: in.Kind, BaseRevisionId: in.BaseRevisionID, Title: in.Title, MediaType: in.MediaType, ObjectKey: objectKey, ContentHash: hash, SourceRefs: refs, Provenance: in.Provenance, CreatedBy: in.Actor, CreatedAt: now()}
	if err = saveJSON(ctx, tx, "INSERT INTO knowledge_revisions(id,module_id,entity_id,kind,data) VALUES($1,$2,$3,$4,$5)", r, r.RevisionId, r.ModuleId, r.EntityId, r.Kind); err != nil {
		return r, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO knowledge_heads(module_id,kind,entity_id,revision_id) VALUES($1,$2,$3,$4) ON CONFLICT(module_id,kind,entity_id) DO UPDATE SET revision_id=excluded.revision_id", r.ModuleId, r.Kind, r.EntityId, r.RevisionId); err != nil {
		return r, err
	}
	return r, emit(ctx, tx, "knowledge.revision.created.v1", r.ModuleId, r)
}

// paragraph:N is stable within an immutable UTF-8 revision. Blank-line-separated blocks are numbered from one.
func validLocator(r types.Revision, locator string) bool {
	// Text and locator use the same immutable content hash.
	var p int
	_, err := fmt.Sscanf(locator, "paragraph:%d", &p)
	blocks := strings.Split(strings.ReplaceAll(r.Content, "\r\n", "\n"), "\n\n")
	count := 0
	for _, block := range blocks {
		if strings.TrimSpace(block) != "" {
			count++
		}
	}
	return err == nil && p > 0 && p <= count && locator == fmt.Sprintf("paragraph:%d", p)
}
func revision(ctx context.Context, q queryer, revisionID string) (types.Revision, error) {
	var raw []byte
	var withdrawn bool
	err := q.QueryRow(ctx, "SELECT data,withdrawn FROM knowledge_revisions WHERE id=$1", revisionID).Scan(&raw, &withdrawn)
	if errors.Is(err, pgx.ErrNoRows) {
		return types.Revision{}, ErrNotFound
	}
	if err != nil {
		return types.Revision{}, err
	}
	var r types.Revision
	err = json.Unmarshal(raw, &r)
	r.Withdrawn = withdrawn
	return r, err
}
func (s *Store) GetRevision(ctx context.Context, revisionID string) (types.Revision, error) {
	r, err := revision(ctx, s.DB, revisionID)
	if err != nil {
		return r, err
	}
	if r.Withdrawn {
		return r, ErrUnavailable
	}
	m, err := module(ctx, s.DB, r.ModuleId, false)
	if err != nil {
		return r, err
	}
	if err = enabled(m); err != nil {
		return r, err
	}
	b, err := s.Objects.Get(ctx, r.ObjectKey, r.ContentHash)
	r.Content = string(b)
	return r, err
}
func (s *Store) Withdraw(ctx context.Context, actor string, req types.WithdrawReq) (types.Receipt, error) {
	return observe(ctx, s, "knowledge.content.withdraw", operationID("withdraw/"+req.ModuleId+"/"+actor, req.IdempotencyKey), req, func(ctx context.Context) (types.Receipt, error) {
		if strings.TrimSpace(req.Reason) == "" {
			return types.Receipt{}, invalid("withdrawal reason required")
		}
		return command(ctx, s, "withdraw/"+req.ModuleId+"/"+actor, req.IdempotencyKey, req, func(tx pgx.Tx) (types.Receipt, error) {
			m, err := module(ctx, tx, req.ModuleId, true)
			if err != nil {
				return types.Receipt{}, err
			}
			switch req.TargetKind {
			case "module":
				if req.TargetId != m.Id {
					return types.Receipt{}, invalid("target module mismatch")
				}
				m.Lifecycle = "WITHDRAWN"
				m.Updated = now()
				err = saveJSON(ctx, tx, "UPDATE knowledge_modules SET data=$2 WHERE id=$1", m, m.Id)
			case "revision":
				r, e := revision(ctx, tx, req.TargetId)
				if e != nil {
					return types.Receipt{}, e
				}
				if r.ModuleId != m.Id {
					return types.Receipt{}, invalid("revision module mismatch")
				}
				_, err = tx.Exec(ctx, "UPDATE knowledge_revisions SET withdrawn=true WHERE id=$1", r.RevisionId)
			default:
				return types.Receipt{}, invalid("target_kind must be module or revision")
			}
			if err != nil {
				return types.Receipt{}, err
			}
			err = emit(ctx, tx, "knowledge.content.withdrawn.v1", m.Id, struct {
				Request types.WithdrawReq `json:"request"`
				Actor   string            `json:"actor"`
			}{req, actor})
			return types.Receipt{Accepted: true}, err
		})

	})
}
