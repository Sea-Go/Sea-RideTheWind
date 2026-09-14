package model

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"

	"github.com/jackc/pgx/v5"
)

// listCursor fixes membership, not mutable execution state. All creation paths
// serialize on the module row before acquiring the table's list_order identity.
// New commits in that module therefore cannot appear below a visible upper bound.
// Clients treat this as opaque; it never substitutes for the query's scope checks.
type listCursor struct {
	Version   int    `json:"v"`
	Kind      string `json:"kind"`
	ModuleID  string `json:"module"`
	ReleaseID string `json:"release,omitempty"`
	Upper     int64  `json:"upper"`
	After     int64  `json:"after"`
}

type recordPage[T any] struct {
	Items      []T
	NextCursor string
}

func decodeCursor(raw, kind, moduleID, releaseID string) (listCursor, error) {
	var c listCursor
	if raw == "" {
		return listCursor{Version: 1, Kind: kind, ModuleID: moduleID, ReleaseID: releaseID}, nil
	}
	if len(raw) > 2048 {
		return c, invalid("cursor too long")
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return c, invalid("invalid cursor encoding")
	}
	decoder := json.NewDecoder(strings.NewReader(string(b)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&c); err != nil {
		return c, invalid("invalid cursor")
	}
	var trailing any
	if err = decoder.Decode(&trailing); err != io.EOF {
		return c, invalid("invalid cursor suffix")
	}
	if c.Version != 1 || c.Kind != kind || c.ModuleID != moduleID || c.ReleaseID != releaseID || c.Upper <= 0 || c.After <= 0 || c.After > c.Upper {
		return c, invalid("cursor scope or position mismatch")
	}
	return c, nil
}

// listRecords uses only a fixed table allowlist and bounded SQL, including the
// extra row used to decide whether a continuation exists. Nil IDs mean all rows;
// a non-nil empty ID set means an empty release, never the whole module.
func listRecords[T any](ctx context.Context, s *Store, kind, moduleID, releaseID string, ids []string, limit int, cursor string) (recordPage[T], error) {
	out := recordPage[T]{Items: []T{}}
	if limit < 1 || limit > 100 {
		return out, invalid("limit must be between 1 and 100")
	}
	payload := "data"
	switch kind {
	case "revisions":
		payload = "jsonb_set(data,'{withdrawn}',to_jsonb(withdrawn))"
	case "releases", "builds", "compiles":
	default:
		return out, invalid("unknown list kind")
	}
	c, err := decodeCursor(cursor, kind, moduleID, releaseID)
	if err != nil {
		return out, err
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(context.Background())
	if _, err = module(ctx, tx, moduleID, false); err != nil {
		return out, err
	}
	table := "knowledge_" + kind
	filter := "module_id=$1 AND ($2::text[] IS NULL OR id=ANY($2))"
	if cursor == "" {
		if err = tx.QueryRow(ctx, "SELECT COALESCE(MAX(list_order),0) FROM "+table+" WHERE "+filter, moduleID, ids).Scan(&c.Upper); err != nil {
			return out, err
		}
	}
	rows, err := tx.Query(ctx, "SELECT "+payload+",list_order FROM "+table+" WHERE "+filter+" AND list_order<=$3 AND ($4::bigint=0 OR list_order<$4) ORDER BY list_order DESC LIMIT $5", moduleID, ids, c.Upper, c.After, limit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		if len(out.Items) == limit {
			raw, err := json.Marshal(c)
			if err != nil {
				return out, err
			}
			out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
			break
		}
		var raw []byte
		if err = rows.Scan(&raw, &c.After); err != nil {
			return out, err
		}
		var value T
		if err = json.Unmarshal(raw, &value); err != nil {
			return out, err
		}
		out.Items = append(out.Items, value)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
