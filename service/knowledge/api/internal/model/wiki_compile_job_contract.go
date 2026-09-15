package model

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/google/uuid"
)

const WikiCompileJobType = "content.wiki-compile.v1"
const WikiCompileResourceProfile = "cpu" // local technical claim only; model routing is separate
const wikiCompileRequestedEvent = "knowledge.wiki.compile.requested.v1"
const wikiCompileCancelledEvent = "knowledge.wiki.compile.cancelled.v1"
const wikiCompileSupersededEvent = "knowledge.wiki.compile.superseded.v1"
const wikiCompileTicketVersion = "rtw.wiki.compile-ticket.v1"

var ErrWikiCompileJobUnavailable = errors.New("DC Wiki Compile job technical service unavailable")
var ErrWikiCompileJobUnauthorized = errors.New("DC Wiki Compile job service authorization rejected")
var ErrWikiCompilePredecessorPending = errors.New("previous Wiki Compile job cancellation pending")

// WikiCompileTicket is a scheduling ticket, not a copy of source content or
// model credentials. BTW must obtain complete guidance and source revisions
// from RTW GetCompile/GetRevision and compare these frozen identities.
type WikiCompileTicket struct {
	SchemaVersion        string   `json:"schema_version"`
	SourceEventID        string   `json:"source_event_id"`
	SourceEventJCSSHA256 string   `json:"source_event_jcs_sha256"`
	CompileID            string   `json:"compile_id"`
	ModuleID             string   `json:"module_id"`
	PageID               string   `json:"page_id"`
	BaseRevisionID       string   `json:"base_revision_id"`
	SourceRevisionIDs    []string `json:"source_revision_ids"`
	GuidanceSHA256       string   `json:"guidance_sha256"`
	CompileInputHash     string   `json:"compile_input_hash"`
	Generation           string   `json:"generation"`
	CancelVersion        string   `json:"cancel_version"`
}

// DC's JCS input_hash covers this entire Submit. It is intentionally distinct
// from RTW Compile.InputHash, which covers business source/guidance fields.
type WikiCompileJobSubmit struct {
	Producer        string          `json:"producer"`
	OperationID     string          `json:"operation_id"`
	RunRef          string          `json:"run_ref"`
	JobType         string          `json:"job_type"`
	ResourceProfile string          `json:"resource_profile"`
	Input           json.RawMessage `json:"input"`
	Deadline        string          `json:"deadline"`
	MaxAttempts     int             `json:"max_attempts"`
}

type WikiCompileSubmissionReceipt struct {
	JobID           string `json:"job_id"`
	Producer        string `json:"producer"`
	OperationID     string `json:"operation_id"`
	InputHash       string `json:"input_hash"`
	TechnicalStatus string `json:"technical_status"`
	ReceivedAt      string `json:"received_at"`
}

type WikiCompileJobSnapshot struct {
	JobID         string               `json:"job_id"`
	InputHash     string               `json:"input_hash"`
	Request       WikiCompileJobSubmit `json:"request"`
	State         string               `json:"technical_state"`
	Attempt       int                  `json:"attempt"`
	LeaseEpoch    int64                `json:"lease_epoch"`
	CancelVersion int64                `json:"cancel_version"`
	AttemptID     string               `json:"attempt_id"`
	WorkerID      string               `json:"worker_id"`
	LeaseExpires  string               `json:"lease_expires_at"`
	Result        json.RawMessage      `json:"result"`
	CreatedAt     string               `json:"created_at"`
	UpdatedAt     string               `json:"updated_at"`
}

type WikiCompileJobCancel struct {
	OperationID           string `json:"operation_id"`
	ExpectedCancelVersion int64  `json:"expected_cancel_version"`
	Reason                string `json:"reason"`
}
type WikiCompileCancelReceipt struct {
	JobID          string `json:"job_id"`
	OperationID    string `json:"operation_id"`
	CancelVersion  int64  `json:"cancel_version"`
	TechnicalState string `json:"technical_state"`
}

type WikiCompileJobTransport interface {
	Submit(context.Context, WikiCompileJobSubmit) (WikiCompileSubmissionReceipt, error)
	Get(context.Context, string) (WikiCompileJobSnapshot, error)
	Cancel(context.Context, string, WikiCompileJobCancel) (WikiCompileCancelReceipt, error)
}

func decodeWikiCompileJSON(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing wiki compile JSON")
	}
	return nil
}

func wikiCompileJCSHash(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	canonical, err := jsoncanonicalizer.Transform(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func wikiCompileEventHash(raw []byte) (string, error) {
	canonical, err := jsoncanonicalizer.Transform(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func validWikiCompileEvent(e Event, expectedType string) bool {
	if e.EventType != expectedType || e.SchemaVersion != 1 || e.Producer != "ridethewind.knowledge" ||
		e.AggregateID == "" || e.AggregateVersion < 1 || !strings.HasPrefix(e.EventID, "evt_") ||
		!strings.HasPrefix(e.OperationID, "command:") || len(e.OperationID) != len("command:")+64 {
		return false
	}
	eventUUID, err := uuid.Parse(strings.TrimPrefix(e.EventID, "evt_"))
	if err != nil || eventUUID.String() != strings.TrimPrefix(e.EventID, "evt_") {
		return false
	}
	operationHash := strings.TrimPrefix(e.OperationID, "command:")
	decoded, err := hex.DecodeString(operationHash)
	if err != nil || hex.EncodeToString(decoded) != operationHash {
		return false
	}
	_, err = time.Parse(time.RFC3339Nano, e.OccurredAt)
	return err == nil
}

func strictWikiCompileSources(ids []string) bool {
	if len(ids) < 1 {
		return false
	}
	for i, id := range ids {
		if id == "" || (i > 0 && ids[i-1] >= id) {
			return false
		}
	}
	return true
}

func wikiCompileBusinessHash(c types.Compile) (string, error) {
	return hashInput(struct {
		ModuleID string   `json:"module_id"`
		PageID   string   `json:"page_id"`
		Base     string   `json:"base_revision_id"`
		Sources  []string `json:"source_revision_ids"`
		Guidance string   `json:"guidance"`
	}{c.ModuleId, c.PageId, c.BaseRevisionId, c.SourceRevisionIds, c.Guidance})
}

func buildWikiCompileSubmit(event Event, raw []byte, c types.Compile) (WikiCompileJobSubmit, WikiCompileTicket, string, error) {
	var empty WikiCompileJobSubmit
	if !validWikiCompileEvent(event, wikiCompileRequestedEvent) || c.CompileId == "" ||
		c.ModuleId != event.AggregateID || c.PageId == "" || c.State != "BUILDING" ||
		c.Generation < 1 || c.CancelVersion != 0 || !strictWikiCompileSources(c.SourceRevisionIds) ||
		strings.TrimSpace(c.Guidance) == "" {
		return empty, WikiCompileTicket{}, "", ErrInvalid
	}
	computed, err := wikiCompileBusinessHash(c)
	if err != nil || computed != c.InputHash {
		return empty, WikiCompileTicket{}, "", ErrInvalid
	}
	eventHash, err := wikiCompileEventHash(raw)
	if err != nil {
		return empty, WikiCompileTicket{}, "", ErrInvalid
	}
	issuedAt, err := time.Parse(time.RFC3339Nano, event.OccurredAt)
	if err != nil {
		return empty, WikiCompileTicket{}, "", ErrInvalid
	}
	ticket := WikiCompileTicket{SchemaVersion: wikiCompileTicketVersion,
		SourceEventID: event.EventID, SourceEventJCSSHA256: eventHash,
		CompileID: c.CompileId, ModuleID: c.ModuleId, PageID: c.PageId,
		BaseRevisionID: c.BaseRevisionId, SourceRevisionIDs: append([]string(nil), c.SourceRevisionIds...),
		GuidanceSHA256: object.Hash([]byte(c.Guidance)), CompileInputHash: c.InputHash,
		Generation: strconv.FormatInt(c.Generation, 10), CancelVersion: "0"}
	input, err := json.Marshal(ticket)
	if err != nil {
		return empty, WikiCompileTicket{}, "", err
	}
	submit := WikiCompileJobSubmit{Producer: event.Producer, OperationID: event.OperationID,
		RunRef: "wiki-compile/" + c.CompileId, JobType: WikiCompileJobType,
		ResourceProfile: WikiCompileResourceProfile, Input: input,
		Deadline: issuedAt.Add(2 * time.Hour).UTC().Format(time.RFC3339Nano), MaxAttempts: 3}
	hash, err := wikiCompileJCSHash(submit)
	if err != nil {
		return empty, WikiCompileTicket{}, "", err
	}
	return submit, ticket, hash, nil
}

func wikiCompileJobMatches(actual WikiCompileJobSnapshot, expected WikiCompileJobSubmit,
	expectedHash, jobID string) bool {
	if actual.JobID != jobID || actual.InputHash != expectedHash || actual.Request.Producer != expected.Producer ||
		actual.Request.OperationID != expected.OperationID || actual.Request.RunRef != expected.RunRef ||
		actual.Request.JobType != expected.JobType || actual.Request.ResourceProfile != expected.ResourceProfile ||
		actual.Request.Deadline != expected.Deadline || actual.Request.MaxAttempts != expected.MaxAttempts {
		return false
	}
	gotHash, err := wikiCompileJCSHash(actual.Request)
	return err == nil && gotHash == expectedHash &&
		(actual.State == "queued" || actual.State == "running" || actual.State == "succeeded" ||
			actual.State == "failed" || actual.State == "cancel_requested" || actual.State == "cancelled")
}
