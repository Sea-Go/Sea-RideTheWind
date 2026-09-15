package model

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/jackc/pgx/v5"
)

// checkWikiH04Predecessor runs before the ordinary Eventing sender has any DC
// effect. SKIP LOCKED may pick a successor while another instance owns its
// older Outbox row; a source receipt must have committed locally first.
// All non-Wiki H04 events keep their former routing and checks.
func checkWikiH04Predecessor(ctx context.Context, tx pgx.Tx, event Event) error {
	switch event.EventType {
	case wikiCompileRequestedEvent:
		if !validWikiCompileEvent(event, wikiCompileRequestedEvent) {
			return invalid("frozen Wiki Compile requested H04 event invalid")
		}
		var compile types.Compile
		if decodeWikiCompileJSON(event.Payload, &compile) != nil ||
			compile.CompileId == "" || compile.ModuleId != event.AggregateID || compile.State != "BUILDING" {
			return invalid("frozen Wiki Compile requested H04 payload invalid")
		}
		var pending int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM knowledge_outbox prior
		 WHERE prior.event_type='knowledge.wiki.compile.superseded.v1'
		 AND prior.payload->'payload'->>'replacement_compile_id'=$1
		 AND prior.delivered_at IS NULL`, compile.CompileId).Scan(&pending); err != nil {
			return err
		}
		if pending != 0 {
			return ErrWikiCompilePredecessorPending
		}
		return nil
	case wikiCompileCancelledEvent, wikiCompileSupersededEvent:
		if !validWikiCompileEvent(event, event.EventType) {
			return invalid("frozen Wiki Compile cancellation H04 event invalid")
		}
		var payload struct {
			Compile               types.Compile `json:"compile"`
			ReplacementCompileID  string        `json:"replacement_compile_id"`
			ReplacementGeneration int64         `json:"replacement_generation"`
			Reason                string        `json:"reason"`
		}
		if decodeWikiCompileJSON(event.Payload, &payload) != nil ||
			payload.Compile.CompileId == "" || payload.Compile.ModuleId != event.AggregateID ||
			payload.Compile.CancelVersion != 1 {
			return invalid("frozen Wiki Compile cancellation H04 payload invalid")
		}
		if event.EventType == wikiCompileSupersededEvent {
			if payload.Compile.State != "SUPERSEDED" || payload.ReplacementCompileID == "" ||
				payload.ReplacementGeneration <= payload.Compile.Generation ||
				payload.Reason != "new_generation" {
				return invalid("frozen Wiki Compile superseded H04 payload invalid")
			}
		} else if payload.Compile.State != "CANCELLED" || payload.Reason == "" ||
			payload.ReplacementCompileID != "" || payload.ReplacementGeneration != 0 {
			return invalid("frozen administrator Wiki Compile cancel H04 payload invalid")
		}
		var count int64
		var allDelivered bool
		if err := tx.QueryRow(ctx, `SELECT count(*),COALESCE(bool_and(delivered_at IS NOT NULL),false)
		 FROM knowledge_outbox prior WHERE prior.event_type='knowledge.wiki.compile.requested.v1'
		 AND prior.payload->'payload'->>'compile_id'=$1`,
			payload.Compile.CompileId).Scan(&count, &allDelivered); err != nil {
			return err
		}
		if count != 1 {
			return conflict("Wiki Compile H04 requested predecessor missing or ambiguous")
		}
		if !allDelivered {
			return ErrWikiCompilePredecessorPending
		}
		return nil
	default:
		return nil
	}
}
