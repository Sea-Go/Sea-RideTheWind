// Package communityfact owns the technical wire shared by RTW community
// domains. Domain packages still own their payload semantics and outboxes.
package communityfact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

var receiptHash = regexp.MustCompile(`^[a-f0-9]{64}$`)

const (
	DeliveryPending int32 = iota
	DeliveryAccepted
	DeliveryFailed
	DeliveryBlocked
)

var ErrFrozenEnvelopeBlocked = errors.New("community fact frozen envelope blocked")
var ErrAuthorityUnavailable = errors.New("community fact authority unavailable")

type Event struct {
	EventID          string          `json:"event_id"`
	EventType        string          `json:"event_type"`
	SchemaVersion    int             `json:"schema_version"`
	Producer         string          `json:"producer"`
	AggregateID      string          `json:"aggregate_id"`
	AggregateVersion int64           `json:"aggregate_version"`
	OperationID      string          `json:"operation_id"`
	OccurredAt       string          `json:"occurred_at"`
	Payload          json.RawMessage `json:"payload"`
}

type TechnicalReceipt struct {
	EventID         string `json:"event_id"`
	Producer        string `json:"producer"`
	TechnicalStatus string `json:"technical_status"`
	ReceiptID       string `json:"receipt_id"`
	InputHash       string `json:"input_hash"`
	Offset          int64  `json:"offset"`
	ReceivedAt      string `json:"received_at"`
}

type SubjectRef struct {
	AuthorityID string `json:"authority_id"`
	TenantID    string `json:"tenant_id"`
	SubjectID   string `json:"subject_id"`
}

type AuthorityFact struct {
	Event              Event            `json:"event"`
	SubjectRef         SubjectRef       `json:"subject_ref"`
	PredecessorEventID string           `json:"predecessor_event_id,omitempty"`
	TechnicalReceipt   TechnicalReceipt `json:"technical_receipt"`
	SourceEventHash    string           `json:"source_event_hash"`
}

type Sender interface {
	Send(context.Context, Event, json.RawMessage) (TechnicalReceipt, error)
}

type DCEventSender struct {
	endpoint string
	token    string
	client   *http.Client
}

func NewDCEventSender(endpoint, token string, client *http.Client) (*DCEventSender, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		parsed.Path != "/v1/events" || len(token) < 32 || client == nil {
		return nil, errors.New("community DataCenter event sender is not configured")
	}
	return &DCEventSender{endpoint: parsed.String(), token: token, client: client}, nil
}

func (s *DCEventSender) Send(ctx context.Context, event Event, raw json.RawMessage) (TechnicalReceipt, error) {
	if s == nil || s.client == nil {
		return TechnicalReceipt{}, errors.New("community DataCenter event sender is not configured")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(raw))
	if err != nil {
		return TechnicalReceipt{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", event.EventID)
	request.Header.Set("Authorization", "Bearer "+s.token)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(request.Header))
	response, err := s.client.Do(request)
	if err != nil {
		return TechnicalReceipt{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return TechnicalReceipt{}, fmt.Errorf("DataCenter event acceptance status %d", response.StatusCode)
	}
	var receipt TechnicalReceipt
	if err := StrictDecode(io.LimitReader(response.Body, 64<<10), &receipt); err != nil {
		return TechnicalReceipt{}, fmt.Errorf("decode DataCenter event receipt: %w", err)
	}
	return receipt, nil
}

func StrictDecode(reader io.Reader, value any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func CanonicalHash(raw []byte) (string, error) {
	canonical, err := jsoncanonicalizer.Transform(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func ValidateReceipt(event Event, receipt TechnicalReceipt) (time.Time, bool) {
	if receipt.EventID != event.EventID || receipt.Producer != event.Producer ||
		receipt.TechnicalStatus != "accepted" || receipt.ReceiptID == "" ||
		!receiptHash.MatchString(receipt.InputHash) || receipt.Offset < 1 {
		return time.Time{}, false
	}
	received, err := time.Parse(time.RFC3339Nano, receipt.ReceivedAt)
	return received.UTC(), err == nil
}

func ValidHash(value string) bool { return receiptHash.MatchString(value) }

func ParseRTWSubject(value string) (SubjectRef, bool) {
	const prefix = "rtw.identity/platform/"
	if !strings.HasPrefix(value, prefix) {
		return SubjectRef{}, false
	}
	id := strings.TrimPrefix(value, prefix)
	uid, err := strconv.ParseInt(id, 10, 64)
	if err != nil || uid <= 0 || strconv.FormatInt(uid, 10) != id {
		return SubjectRef{}, false
	}
	return SubjectRef{AuthorityID: "rtw.identity", TenantID: "platform", SubjectID: id}, true
}
