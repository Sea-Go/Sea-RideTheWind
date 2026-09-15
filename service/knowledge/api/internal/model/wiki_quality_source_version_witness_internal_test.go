package model

import (
	"encoding/json"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/types"
)

func TestSourceVersionWithdrawalEventKeepsTypedTargetAndWholeJCS(t *testing.T) {
	const moduleID = "module_source_version_test"
	const eventID = "evt_source_version_withdrawal_test"
	request := types.WithdrawReq{ModuleId: moduleID, TargetKind: "revision",
		TargetId: "revision_source_test", Reason: "source withdrawn",
		IdempotencyKey: "withdraw-source-test"}
	payload, err := json.Marshal(struct {
		Request types.WithdrawReq `json:"request"`
		Actor   string            `json:"actor"`
	}{request, "test-admin"})
	if err != nil {
		t.Fatal(err)
	}
	event := Event{AggregateVersion: 3, OperationID: "command:withdraw-source-test",
		EventID: eventID, EventType: "knowledge.content.withdrawn.v1",
		SchemaVersion: 1, Producer: "ridethewind.knowledge",
		AggregateID: moduleID, OccurredAt: "2026-09-16T00:00:00Z", Payload: payload}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	wantHash, err := wikiQualityJCSHash(raw)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := sourceVersionEventFromJSONB(raw, eventID, event.EventType,
		moduleID, moduleID, time.Date(2026, 9, 16, 1, 2, 3, 0, time.UTC))
	if err != nil || entry.EventJCSSHA256 != wantHash || entry.AggregateVersion != 3 ||
		entry.TargetKind != "revision" || entry.TargetID != request.TargetId ||
		entry.JCSSource != "outbox_jsonb_canonical_at_read" || entry.DeliveredAt == "" {
		t.Fatalf("withdrawal source Event lost typed target/JCS: %+v %v", entry, err)
	}
	bad := []struct {
		name string
		raw  []byte
		id   string
		kind string
		mod  string
		time time.Time
	}{
		{"table EventID drift", raw, "evt_other", event.EventType, moduleID, time.Now()},
		{"table type drift", raw, eventID, "knowledge.other.v1", moduleID, time.Now()},
		{"wrong module", raw, eventID, event.EventType, "module_other", time.Now()},
		{"not delivered", raw, eventID, event.EventType, moduleID, time.Time{}},
		{"unknown envelope key", append(append([]byte(nil), raw[:len(raw)-1]...), []byte(`,"tenant_id":"forbidden"}`)...),
			eventID, event.EventType, moduleID, time.Now()},
	}
	for _, test := range bad {
		t.Run(test.name, func(t *testing.T) {
			if _, err := sourceVersionEventFromJSONB(test.raw, test.id, test.kind,
				moduleID, test.mod, test.time); err == nil {
				t.Fatal("mutated or undelivered source Event was accepted")
			}
		})
	}
}
