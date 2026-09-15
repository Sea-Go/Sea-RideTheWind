package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/object"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

// This is a test-only witness for DataCenter's acknowledged-prefix v1. The
// typed DC input_hash identifies its canonical Event, whereas rawSHA identifies
// RTW's original transport bytes. The two names must never be interchanged.
var errRealDCPrefix = errors.New("real Wiki FactSet DC acknowledged prefix is incomplete or contradictory")

const realDCPrefixConsumer = "btw-warehouse-wiki-quality"
const realDCPrefixProducer = "ridethewind.knowledge"

type realDCPin struct {
	OriginalRawSHA string
	OriginalJCSSHA string
	EventType      string
	Offset         int64
}

type realDCEvent struct {
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

type realDCEventItem struct {
	Offset    int64       `json:"offset"`
	InputHash string      `json:"input_hash"`
	Event     realDCEvent `json:"event"`
}

type realDCAckItem struct {
	Offset     int64       `json:"offset"`
	EventID    string      `json:"event_id"`
	InputHash  string      `json:"input_hash"`
	ReceiptID  string      `json:"receipt_id"`
	ReceivedAt string      `json:"received_at"`
	Event      realDCEvent `json:"event"`
}

type realDCBatchReceipt struct {
	FromOffset int64  `json:"from_offset"`
	ToOffset   int64  `json:"to_offset"`
	BatchHash  string `json:"batch_hash"`
	AcceptedAt string `json:"accepted_at"`
}

type realDCAckPage struct {
	Consumer           string               `json:"consumer"`
	Producer           string               `json:"producer"`
	CutoffOffset       int64                `json:"cutoff_offset"`
	AcknowledgedOffset int64                `json:"acknowledged_offset"`
	ProducerOffset     int64                `json:"producer_offset"`
	FromOffset         int64                `json:"from_offset"`
	ToOffset           int64                `json:"to_offset"`
	PrefixVerified     bool                 `json:"prefix_verified"`
	Events             []realDCAckItem      `json:"events"`
	DeliveryReceipts   []realDCBatchReceipt `json:"delivery_receipts"`
}

type realDCPrefixProof struct {
	Cutoff              int64
	AcknowledgedAtLeast int64
	IndexJCSSHA         string
	BatchReceipts       int
	OriginalEventInputs map[string]string
	FullPrefixVerified  bool
}

func pinRealDCRTWEvent(raw string, originalRawSHA, originalJCSSHA, eventType string,
	offset int64) (realDCPin, error) {
	var pin realDCPin
	if raw == "" || !realDCSHA(originalRawSHA) || !realDCSHA(originalJCSSHA) ||
		eventType == "" || offset < 1 || object.Hash([]byte(raw)) != originalRawSHA {
		return pin, errRealDCPrefix
	}
	canon, err := jsoncanonicalizer.Transform([]byte(raw))
	if err != nil || object.Hash(canon) != originalJCSSHA {
		return pin, errRealDCPrefix
	}
	return realDCPin{OriginalRawSHA: originalRawSHA, OriginalJCSSHA: originalJCSSHA,
		EventType: eventType, Offset: offset}, nil
}

// The actual DC platform must be contacted after BTW has committed and ACKed
// the full producer prefix. No query here creates a consumer or moves a cursor.
func readRealDCAcknowledgedWikiFactSetPrefix(ctx context.Context, baseURL, token string,
	cutoff int64, pins map[string]realDCPin) (realDCPrefixProof, error) {
	var out realDCPrefixProof
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed == nil || parsed.Scheme != "http" ||
		parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" ||
		parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" ||
		parsed.Fragment != "" || token == "" || strings.TrimSpace(token) != token ||
		strings.ContainsAny(token, "\r\n") || cutoff < 1 || len(pins) != 4 {
		return out, errRealDCPrefix
	}
	client := &http.Client{Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}
	items := make([]realDCAckItem, 0, cutoff)
	receipts := make(map[int64]realDCBatchReceipt)
	minimumACK := int64(0)
	for from := int64(1); from <= cutoff; {
		query := url.Values{"producer": {realDCPrefixProducer},
			"cutoff":      {strconv.FormatInt(cutoff, 10)},
			"from_offset": {strconv.FormatInt(from, 10)}, "limit": {"128"}}
		endpoint := baseURL + "/v1/event-consumers/" + realDCPrefixConsumer +
			"/acknowledged-prefix?" + query.Encode()
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if reqErr != nil {
			return out, reqErr
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, requestErr := client.Do(req)
		if requestErr != nil {
			return out, requestErr
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return out, fmt.Errorf("%w: DC HTTP status %d", errRealDCPrefix, resp.StatusCode)
		}
		var page realDCAckPage
		decoder := json.NewDecoder(io.LimitReader(resp.Body, 4<<20))
		decoder.DisallowUnknownFields()
		decodeErr := decoder.Decode(&page)
		if decodeErr == nil && decoder.Decode(new(any)) != io.EOF {
			decodeErr = errRealDCPrefix
		}
		closeErr := resp.Body.Close()
		if decodeErr != nil || closeErr != nil {
			return out, errRealDCPrefix
		}
		if !page.PrefixVerified || page.Consumer != realDCPrefixConsumer ||
			page.Producer != realDCPrefixProducer || page.CutoffOffset != cutoff ||
			page.AcknowledgedOffset < cutoff || page.ProducerOffset < cutoff ||
			page.FromOffset != from || page.ToOffset < from || page.ToOffset > cutoff ||
			page.ToOffset-from >= 128 ||
			int64(len(page.Events)) != page.ToOffset-from+1 ||
			len(page.DeliveryReceipts) == 0 {
			return out, errRealDCPrefix
		}
		if minimumACK == 0 || page.AcknowledgedOffset < minimumACK {
			minimumACK = page.AcknowledgedOffset
		}
		for i, item := range page.Events {
			expectedOffset := from + int64(i)
			if item.Offset != expectedOffset || item.EventID == "" ||
				item.Event.EventID != item.EventID ||
				item.Event.Producer != realDCPrefixProducer ||
				item.Event.EventType == "" || item.ReceiptID == "" ||
				!realDCSHA(item.InputHash) ||
				!realDCTime(item.ReceivedAt) {
				return out, errRealDCPrefix
			}
			raw, err := json.Marshal(item.Event)
			if err != nil {
				return out, errRealDCPrefix
			}
			canon, err := jsoncanonicalizer.Transform(raw)
			if err != nil || object.Hash(canon) != item.InputHash {
				return out, errRealDCPrefix
			}
			if pin, found := pins[item.EventID]; found &&
				(pin.Offset != item.Offset || pin.EventType != item.Event.EventType ||
					pin.OriginalJCSSHA != item.InputHash) {
				return out, errRealDCPrefix
			}
			items = append(items, item)
		}
		for _, receipt := range page.DeliveryReceipts {
			if receipt.FromOffset < 1 || receipt.ToOffset < receipt.FromOffset ||
				receipt.ToOffset-receipt.FromOffset >= 128 ||
				receipt.FromOffset > page.ToOffset || receipt.ToOffset < page.FromOffset ||
				!realDCSHA(receipt.BatchHash) || !realDCTime(receipt.AcceptedAt) {
				return out, errRealDCPrefix
			}
			if old, found := receipts[receipt.FromOffset]; found && old != receipt {
				return out, errRealDCPrefix
			}
			receipts[receipt.FromOffset] = receipt
		}
		from = page.ToOffset + 1
	}
	if len(items) != int(cutoff) {
		return out, errRealDCPrefix
	}
	seen := make(map[string]bool, len(items))
	inputByID := make(map[string]string, len(pins))
	for _, item := range items {
		if seen[item.EventID] {
			return out, errRealDCPrefix
		}
		seen[item.EventID] = true
		if _, pinned := pins[item.EventID]; pinned {
			inputByID[item.EventID] = item.InputHash
		}
	}
	if len(inputByID) != len(pins) {
		return out, errRealDCPrefix
	}
	for from := int64(1); from <= cutoff; {
		receipt, found := receipts[from]
		if !found || receipt.ToOffset > cutoff {
			return out, errRealDCPrefix
		}
		batch := make([]realDCEventItem, 0, receipt.ToOffset-from+1)
		for offset := from; offset <= receipt.ToOffset; offset++ {
			item := items[offset-1]
			batch = append(batch, realDCEventItem{Offset: item.Offset,
				InputHash: item.InputHash, Event: item.Event})
		}
		_, actualSHA, err := realDCJCS(batch)
		if err != nil || actualSHA != receipt.BatchHash {
			return out, errRealDCPrefix
		}
		out.BatchReceipts++
		from = receipt.ToOffset + 1
	}
	_, indexSHA, err := realDCJCS(items)
	if err != nil {
		return out, errRealDCPrefix
	}
	out.Cutoff, out.AcknowledgedAtLeast, out.IndexJCSSHA,
		out.OriginalEventInputs, out.FullPrefixVerified =
		cutoff, minimumACK, indexSHA, inputByID, true
	return out, nil
}

func realDCJCS(value any) ([]byte, string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	canon, err := jsoncanonicalizer.Transform(raw)
	if err != nil {
		return nil, "", err
	}
	return canon, object.Hash(canon), nil
}

func realDCSHA(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func realDCTime(value string) bool {
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}

// The test fixture needs the same Event bytes as the producer, not a DC
// projection reconstructed from source-side typed fields.
func realDCRTWEventFromOriginal(raw string) (realDCEvent, error) {
	var event realDCEvent
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&event) != nil || decoder.Decode(new(any)) != io.EOF ||
		event.EventID == "" || event.Producer != realDCPrefixProducer {
		return realDCEvent{}, errRealDCPrefix
	}
	if _, sha, err := realDCJCS(event); err != nil || !realDCSHA(sha) {
		return realDCEvent{}, errRealDCPrefix
	}
	return event, nil
}

func realDCOriginalMatchesTyped(raw string, event realDCEvent) bool {
	original, err := jsoncanonicalizer.Transform([]byte(raw))
	if err != nil {
		return false
	}
	typed, _, err := realDCJCS(event)
	return err == nil && bytes.Equal(original, typed)
}

func TestWikiFactSetDCFullPrefixReadOnlyWitnessRejectsMissingOrMutatedEvidence(t *testing.T) {
	const testToken = "test-only-fact-set-dc-prefix-token"
	const now = "2026-09-16T00:00:00Z"
	page := realDCAckPage{Consumer: realDCPrefixConsumer,
		Producer: realDCPrefixProducer, CutoffOffset: 4,
		AcknowledgedOffset: 4, ProducerOffset: 4,
		FromOffset: 1, ToOffset: 4, PrefixVerified: true}
	pins := make(map[string]realDCPin, 4)
	for i, kind := range []string{
		"knowledge.wiki.quality.judged.v1", "knowledge.wiki.fact-set.frozen.v1",
		"knowledge.wiki.quality.judged.v1", "knowledge.wiki.quality.judged.v1"} {
		id := fmt.Sprintf("fixture-judgment-%d", i+1)
		event := realDCEvent{EventID: id, EventType: kind, SchemaVersion: 1,
			Producer: realDCPrefixProducer, AggregateID: "wiki-fixture",
			AggregateVersion: int64(i + 1), OperationID: id, OccurredAt: now,
			Payload: json.RawMessage(`{"fact_id":"fixture"}`)}
		originalBytes, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		original := string(originalBytes)
		if !realDCOriginalMatchesTyped(original, event) {
			t.Fatal("fixture original RTW Event and DC typed wire differ")
		}
		originalEvent, err := realDCRTWEventFromOriginal(original)
		if err != nil || originalEvent.EventID != id {
			t.Fatal("fixture is not an original nine-field RTW Event")
		}
		_, jcsSHA, err := realDCJCS(originalEvent)
		if err != nil {
			t.Fatal(err)
		}
		pin, err := pinRealDCRTWEvent(original, object.Hash(originalBytes),
			jcsSHA, kind, int64(i+1))
		if err != nil {
			t.Fatal(err)
		}
		pins[id] = pin
		page.Events = append(page.Events, realDCAckItem{Offset: int64(i + 1),
			EventID: id, InputHash: jcsSHA,
			ReceiptID: fmt.Sprintf("receipt-%d", i+1), ReceivedAt: now,
			Event: originalEvent})
	}
	batch := make([]realDCEventItem, 0, 4)
	for _, item := range page.Events {
		batch = append(batch, realDCEventItem{Offset: item.Offset,
			InputHash: item.InputHash, Event: item.Event})
	}
	_, batchSHA, err := realDCJCS(batch)
	if err != nil {
		t.Fatal(err)
	}
	page.DeliveryReceipts = []realDCBatchReceipt{{FromOffset: 1,
		ToOffset: 4, BatchHash: batchSHA, AcceptedAt: now}}
	read := func(value realDCAckPage, status int, requireToken bool,
		sourcePins map[string]realDCPin) (realDCPrefixProof, error) {
		t.Helper()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/event-consumers/btw-warehouse-wiki-quality/acknowledged-prefix" ||
				r.URL.Query().Get("producer") != realDCPrefixProducer ||
				r.URL.Query().Get("cutoff") != "4" ||
				r.URL.Query().Get("from_offset") != "1" ||
				r.URL.Query().Get("limit") != "128" {
				t.Errorf("Holder used wrong DC read-only route/query: %s", r.URL)
			}
			if requireToken && r.Header.Get("Authorization") != "Bearer "+testToken {
				t.Error("DC read lacks fixed service token")
			}
			w.WriteHeader(status)
			if status == http.StatusOK {
				if err := json.NewEncoder(w).Encode(value); err != nil {
					t.Errorf("DC fixture encode: %v", err)
				}
			}
		}))
		defer server.Close()
		return readRealDCAcknowledgedWikiFactSetPrefix(context.Background(),
			server.URL, testToken, 4, sourcePins)
	}
	good, err := read(page, http.StatusOK, true, pins)
	if err != nil || !good.FullPrefixVerified || good.Cutoff != 4 ||
		good.AcknowledgedAtLeast != 4 || good.BatchReceipts != 1 ||
		len(good.OriginalEventInputs) != 4 || !realDCSHA(good.IndexJCSSHA) {
		t.Fatalf("one true ACK batch and four original RTW Event hashes rejected: %+v %v", good, err)
	}
	badInput := page
	badInput.Events = append([]realDCAckItem(nil), page.Events...)
	badInput.Events[2].InputHash = strings.Repeat("0", 64)
	badReceipt := page
	badReceipt.DeliveryReceipts = []realDCBatchReceipt{{FromOffset: 1,
		ToOffset: 4, BatchHash: strings.Repeat("0", 64), AcceptedAt: now}}
	badGap := page
	badGap.Events = append([]realDCAckItem(nil), page.Events...)
	badGap.Events[1].Offset = 3
	badPin := make(map[string]realDCPin, len(pins))
	for id, pin := range pins {
		badPin[id] = pin
	}
	modified := badPin[page.Events[2].EventID]
	modified.OriginalJCSSHA = strings.Repeat("0", 64)
	badPin[page.Events[2].EventID] = modified
	badACK := page
	badACK.PrefixVerified = false
	for _, tc := range []struct {
		name   string
		page   realDCAckPage
		status int
		pins   map[string]realDCPin
	}{
		{"unacknowledged HTTP route", page, http.StatusConflict, pins},
		{"typed DC input hash altered", badInput, http.StatusOK, pins},
		{"batch ACK receipt altered", badReceipt, http.StatusOK, pins},
		{"offset gap", badGap, http.StatusOK, pins},
		{"source signed original JCS altered", page, http.StatusOK, badPin},
		{"DC did not verify full prefix", badACK, http.StatusOK, pins},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proof, err := read(tc.page, tc.status, true, tc.pins)
			if !errors.Is(err, errRealDCPrefix) || proof.FullPrefixVerified {
				t.Fatalf("contradictory DC acknowledgement passed: %+v %v", proof, err)
			}
		})
	}
}
