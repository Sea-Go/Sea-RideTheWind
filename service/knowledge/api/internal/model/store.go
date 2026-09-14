package model

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/telemetry"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string

var (
	ErrNotFound    = errors.New("knowledge resource not found")
	ErrConflict    = errors.New("knowledge state conflict")
	ErrInvalid     = errors.New("invalid knowledge input")
	ErrUnavailable = errors.New("knowledge resource unavailable")
)

type Store struct {
	DB            *pgxpool.Pool
	Objects       object.Store
	Observability *telemetry.Runtime
}

func New(db *pgxpool.Pool, objects object.Store, options ...Option) *Store {
	s := &Store{DB: db, Objects: objects}
	for _, option := range options {
		option(s)
	}
	return s
}
func (s *Store) Migrate(ctx context.Context) error { _, err := s.DB.Exec(ctx, schema); return err }
func id(prefix string) string                      { return prefix + "_" + uuid.NewString() }
func now() string                                  { return time.Now().UTC().Format(time.RFC3339Nano) }
func invalid(why string) error                     { return fmt.Errorf("%w: %s", ErrInvalid, why) }
func conflict(why string) error                    { return fmt.Errorf("%w: %s", ErrConflict, why) }

type reasonError struct {
	Cause   error
	Code    string
	Message string
}

func (e *reasonError) Error() string { return e.Cause.Error() + ": " + e.Message }
func (e *reasonError) Unwrap() error { return e.Cause }
func conflictCode(code, why string) error {
	return &reasonError{Cause: ErrConflict, Code: code, Message: why}
}
func encode(v any) ([]byte, error) { return json.Marshal(v) }
func hashInput(v any) (string, error) {
	b, err := encode(v)
	if err != nil {
		return "", err
	}
	return object.Hash(b), nil
}

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readJSON[T any](ctx context.Context, q queryer, sql string, args ...any) (T, error) {
	var result T
	var raw []byte
	err := q.QueryRow(ctx, sql, args...).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(raw, &result)
	return result, err
}
func saveJSON(ctx context.Context, tx pgx.Tx, sql string, v any, args ...any) error {
	b, err := encode(v)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, sql, append(args, b)...)
	return err
}

// saveExecutionResult checks the execution lease at the authoritative write,
// after potentially slow artifact verification. A rejected write rolls back
// any revision/head/outbox changes made earlier in this transaction.
func saveExecutionResult(ctx context.Context, tx pgx.Tx, table, id string, v any, liveLeaseRequired bool) error {
	if table != "knowledge_builds" && table != "knowledge_compiles" {
		return invalid("unknown execution table")
	}
	raw, err := encode(v)
	if err != nil {
		return err
	}
	statement := "UPDATE " + table + " SET data=$2 WHERE id=$1"
	if liveLeaseRequired {
		statement += " AND (data->>'lease_expires_at')::timestamptz > clock_timestamp()"
	}
	tag, err := tx.Exec(ctx, statement, id, raw)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return conflictCode("LEASE_EXPIRED", "execution lease expired at result submission")
	}
	return nil
}

// command serializes idempotency keys and commits the domain state, outbox and replay together.
func command[T any](ctx context.Context, s *Store, scope, key string, input any, fn func(pgx.Tx) (T, error)) (T, error) {
	var zero T
	if strings.TrimSpace(key) == "" || len(key) > 200 {
		return zero, invalid("idempotency key required and limited to 200 bytes")
	}
	h, err := hashInput(input)
	if err != nil {
		return zero, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", scope+"/"+key); err != nil {
		return zero, err
	}
	var previousHash string
	var raw []byte
	err = tx.QueryRow(ctx, "SELECT input_hash,response FROM knowledge_operations WHERE scope=$1 AND operation_key=$2", scope, key).Scan(&previousHash, &raw)
	if err == nil {
		if previousHash != h {
			return zero, conflictCode("IDEMPOTENCY_CONFLICT", "idempotency key reused with different input")
		}
		if err = json.Unmarshal(raw, &zero); err != nil {
			return zero, err
		}
		telemetry.Replay(ctx)
		return zero, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return zero, err
	}
	if err = setOperation(ctx, tx, "command:"+object.Hash([]byte(scope+"/"+key))); err != nil {
		return zero, err
	}
	result, err := fn(tx)
	if err != nil {
		return zero, err
	}
	raw, err = encode(result)
	if err != nil {
		return zero, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO knowledge_operations(scope,operation_key,input_hash,response) VALUES($1,$2,$3,$4)", scope, key, h, raw); err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return result, nil
}
func module(ctx context.Context, q queryer, moduleID string, lock bool) (types.Module, error) {
	sql := "SELECT data FROM knowledge_modules WHERE id=$1"
	if lock {
		sql += " FOR UPDATE"
	}
	return readJSON[types.Module](ctx, q, sql, moduleID)
}
func enabled(m types.Module) error {
	if m.Lifecycle != "ENABLED" {
		return ErrUnavailable
	}
	return nil
}
func setOperation(ctx context.Context, tx pgx.Tx, operationID string) error {
	_, err := tx.Exec(ctx, "SELECT set_config('knowledge.operation_id',$1,true)", operationID)
	return err
}
func emit(ctx context.Context, tx pgx.Tx, eventType, aggregate string, payload any) error {
	var sequence int64
	if err := tx.QueryRow(ctx, "UPDATE knowledge_modules SET event_sequence=event_sequence+1 WHERE id=$1 RETURNING event_sequence", aggregate).Scan(&sequence); err != nil {
		return err
	}
	var operationID string
	if err := tx.QueryRow(ctx, "SELECT current_setting('knowledge.operation_id',true)").Scan(&operationID); err != nil {
		return err
	}
	if operationID == "" {
		return invalid("event operation identity missing")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	event := Event{EventID: id("evt"), EventType: eventType, SchemaVersion: 1, Producer: "ridethewind.knowledge", AggregateID: aggregate, AggregateVersion: sequence, OperationID: operationID, OccurredAt: now(), Payload: raw}
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return err
	}
	correlation := telemetry.Capture(ctx)
	encoded, err := json.Marshal(correlation)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO knowledge_outbox(event_id,event_type,aggregate_id,payload,correlation) VALUES($1,$2,$3,$4,$5)", event.EventID, eventType, aggregate, eventJSON, encoded)
	return err
}
func (s *Store) CreateModule(ctx context.Context, actor string, req types.CreateModuleReq) (types.Module, error) {
	return observe(ctx, s, "knowledge.module.create", operationID("module/create/"+actor, req.IdempotencyKey), req, func(ctx context.Context) (types.Module, error) {
		if strings.TrimSpace(req.Title) == "" {
			return types.Module{}, invalid("title required")
		}
		return command(ctx, s, "module/create/"+actor, req.IdempotencyKey, req, func(tx pgx.Tx) (types.Module, error) {
			m := types.Module{Id: id("module"), Title: req.Title, Description: req.Description, Category: req.Category, Image: "mountain", Lifecycle: "ENABLED", Updated: now(), Release: "未发布"}
			err := saveJSON(ctx, tx, "INSERT INTO knowledge_modules(id,data) VALUES($1,$2)", m, m.Id)
			return m, err
		})

	})
}
func (s *Store) GetModule(ctx context.Context, moduleID string, publishedOnly bool) (types.Module, error) {
	if !publishedOnly {
		return module(ctx, s.DB, moduleID, false)
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return types.Module{}, err
	}
	defer tx.Rollback(context.Background())
	m, err := module(ctx, tx, moduleID, false)
	if err != nil {
		return m, err
	}
	if _, err = s.published(ctx, tx, m); err != nil {
		return types.Module{}, err
	}
	return m, tx.Commit(ctx)
}
func (s *Store) ListModules(ctx context.Context, limit int, cursor string, publishedOnly bool) (types.ListModulesResp, error) {
	out := types.ListModulesResp{Items: []types.Module{}}
	if limit < 1 || limit > 100 {
		return out, invalid("limit must be between 1 and 100")
	}
	rows, err := s.DB.Query(ctx, "SELECT data FROM knowledge_modules WHERE id>$1 AND ($2=false OR (data->>'lifecycle'='ENABLED' AND data->>'active_release_id'<>'')) ORDER BY id", cursor, publishedOnly)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return out, err
		}
		var m types.Module
		if err = json.Unmarshal(raw, &m); err != nil {
			return out, err
		}
		if publishedOnly {
			m, err = s.GetModule(ctx, m.Id, true)
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnavailable) {
				continue
			}
			if err != nil {
				return out, err
			}
		}
		if len(out.Items) == limit {
			out.NextCursor = out.Items[len(out.Items)-1].Id
			break
		}
		out.Items = append(out.Items, m)
	}
	return out, rows.Err()
}
