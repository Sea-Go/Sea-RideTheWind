package model

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/telemetry"
	"sea-try-go/service/knowledge/api/internal/types"
)

type Option func(*Store)

func WithObservability(runtime *telemetry.Runtime) Option {
	return func(s *Store) { s.Observability = runtime }
}

// WithContinuousSubjectRefV2Writes is an explicit stage-three storage
// candidate. The caller must apply the guarded four-table sidecar migration
// first. The ordinary Store and schema migration never enable it.
func WithContinuousSubjectRefV2Writes() Option {
	return func(s *Store) { s.continuousSubjectRefV2Writes = true }
}

// WithWikiCompileJobs routes only compile requested/cancelled Outbox events to
// the DC Jobs technical adapter. Omitted keeps the existing H04 event sender.
func WithWikiCompileJobs() Option {
	return func(s *Store) { s.wikiCompileJobs = true }
}
func operationID(scope, key string) string { return "command:" + object.Hash([]byte(scope+"/"+key)) }
func activationKey(req types.ActivateReq) string {
	if req.IdempotencyKey != "" {
		return req.IdempotencyKey
	}
	return fmt.Sprintf("pointer:%d", req.ExpectedPointerRevision)
}
func observe[T any](ctx context.Context, s *Store, event, operation string, input any, fn func(context.Context) (T, error)) (T, error) {
	ctx, finish := s.Observability.Begin(ctx, event, operation, observationFields(input))
	value, err := fn(ctx)
	info := telemetry.ErrorInfo{}
	if err != nil {
		info = ClassifyError(err)
	}
	finish(err, info, observationFields(value))
	return value, err
}

// ClassifyError registers bounded labels and preserves the actual error chain.
func ClassifyError(err error) telemetry.ErrorInfo {
	info := telemetry.Unexpected(err)
	var reason *reasonError
	switch {
	case errors.Is(err, context.Canceled):
		info.Code = "CANCELLED"
		info.Type = "context.Canceled"
		info.Outcome = "cancelled"
		info.Level = "warn"
	case errors.Is(err, context.DeadlineExceeded):
		info.Code = "TIMEOUT"
		info.Type = "context.DeadlineExceeded"
		info.Outcome = "timeout"
		info.Level = "warn"
	case errors.Is(err, ErrConflict):
		info.Code = "STATE_CONFLICT"
		info.Type = "knowledge.ErrConflict"
		info.Outcome = "rejected"
		info.Level = "warn"
	case errors.Is(err, ErrInvalid):
		info.Code = "INVALID_INPUT"
		info.Type = "knowledge.ErrInvalid"
		info.Outcome = "rejected"
		info.Level = "warn"
	case errors.Is(err, ErrWikiCompileJobUnauthorized):
		info.Code = "DC_JOB_AUTH_FAILED"
		info.Type = "knowledge.ErrWikiCompileJobUnauthorized"
		info.Outcome = "rejected"
		info.Level = "warn"
	case errors.Is(err, ErrWikiCompileJobUnavailable):
		info.Code = "DC_JOB_UNAVAILABLE"
		info.Type = "knowledge.ErrWikiCompileJobUnavailable"
		info.Outcome = "failed"
	case errors.Is(err, ErrNotFound):
		info.Code = "NOT_FOUND"
		info.Type = "knowledge.ErrNotFound"
		info.Outcome = "rejected"
		info.Level = "warn"
	case errors.Is(err, ErrArtifactUnavailable):
		info.Code = "ARTIFACT_UNAVAILABLE"
		info.Type = "knowledge.ErrArtifactUnavailable"
		info.Outcome = "failed"
	case errors.Is(err, ErrUnavailable):
		info.Code = "CONTENT_UNAVAILABLE"
		info.Type = "knowledge.ErrUnavailable"
		info.Outcome = "rejected"
		info.Level = "warn"
	}
	if errors.As(err, &reason) {
		info.Code = reason.Code
	}
	return info
}

var observedFields = map[string]string{"ModuleId": "module_id", "ModuleID": "module_id", "ReleaseId": "release_id", "RevisionId": "revision_id", "ContentRevisionId": "content_revision_id", "BaseRevisionId": "base_revision_id", "JudgmentId": "judgment_id", "EventId": "event_id", "KeyId": "key_id", "CaseSha256": "case_sha256", "ReviewSha256": "review_sha256", "RegistryRevision": "registry_revision", "BuildId": "build_id", "CompileId": "compile_id", "Generation": "generation", "AttemptId": "attempt_id", "LeaseEpoch": "lease_epoch", "CancelVersion": "cancel_version", "ExpectedPointerRevision": "expected_pointer_revision", "PointerRevision": "pointer_revision", "InputHash": "input_hash", "ManifestHash": "manifest_hash", "IndexManifestHash": "artifact_hash", "IndexManifestRef": "artifact_ref", "State": "state", "ActiveReleaseId": "active_release_id", "ActiveBuildId": "active_build_id", "TargetKind": "target_kind", "TargetId": "target_id", "Kind": "revision_kind", "SearchId": "search_id", "ChunkId": "chunk_id", "PackHash": "pack_hash", "PublicationRevision": "publication_revision", "AnswerId": "answer_id", "SessionId": "session_id", "AcceptedOrdinal": "accepted_ordinal"}

// Only fixed scalar identity/version fields are read. Request bodies, source
// text and source-ref arrays are not marshaled just to extract log attributes.
func observationFields(value any) map[string]any {
	fields := map[string]any{}
	v := reflect.ValueOf(value)
	if !v.IsValid() {
		return fields
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return fields
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return fields
	}
	for name, key := range observedFields {
		f := v.FieldByName(name)
		if !f.IsValid() {
			continue
		}
		switch f.Kind() {
		case reflect.String:
			if f.String() != "" {
				fields[key] = f.String()
			}
		case reflect.Int, reflect.Int64:
			fields[key] = f.Int()
		case reflect.Bool:
			fields[key] = f.Bool()
		}
	}
	if m, ok := value.(types.Module); ok {
		fields["module_id"] = m.Id
		fields["pointer_revision"] = m.PointerRevision
	}
	return fields
}
func actualFields(ctx context.Context, value any) {
	fields := observationFields(value)
	result := map[string]any{}
	for k, v := range fields {
		result["actual_"+k] = v
	}
	telemetry.Add(ctx, result)
}
