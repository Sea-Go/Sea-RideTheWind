package model_test

import (
	"context"
	"os"
	"testing"

	"sea-try-go/service/knowledge/api/internal/testenv"

	"github.com/jackc/pgx/v5"
)

func TestSubjectRefV2StorageKeepsOldAndControlledHistoryReads(t *testing.T) {
	store := testenv.Store(t)
	fixture := makeCitationFixture(t, store)
	citation := makeCitationRequest(t, fixture, "storage-history-search")
	receipt, err := store.AcceptSearchCitations(context.Background(), citation)
	if err != nil {
		t.Fatal(err)
	}
	old := canonicalHistoryRequest(t, receipt, citation, "storage-history-answer", "platform", "42")
	if _, err = store.CommitAcceptedAnswer(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	beforeV1, err := store.GetAcceptedAnswer(context.Background(), old.Subject, old.SessionId, old.AnswerId)
	if err != nil {
		t.Fatal(err)
	}
	beforeV2, err := store.GetVerifiedProductAcceptedAnswer(context.Background(), old.Subject, old.SessionId, old.AnswerId)
	if err != nil {
		t.Fatal(err)
	}
	var sidecar *string
	if err = store.DB.QueryRow(context.Background(), "SELECT to_regclass('knowledge_accepted_answers_subject_v2')::text").Scan(&sidecar); err != nil || sidecar != nil {
		t.Fatalf("ordinary Store.Migrate activated candidate storage: %v %v", sidecar, err)
	}
	raw, err := os.ReadFile("../../../scripts/migrate-subjectref-v2-storage.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = store.DB.Exec(context.Background(), string(raw), pgx.QueryExecModeSimpleProtocol); err != nil {
			t.Fatalf("explicit candidate projection round %d: %v", i, err)
		}
	}
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatalf("ordinary schema replay after candidate projection: %v", err)
	}
	afterV1, err := store.GetAcceptedAnswer(context.Background(), old.Subject, old.SessionId, old.AnswerId)
	if err != nil || afterV1 != beforeV1 {
		t.Fatalf("old default history read changed: %+v %v", afterV1, err)
	}
	afterV2, err := store.GetVerifiedProductAcceptedAnswer(context.Background(), old.Subject, old.SessionId, old.AnswerId)
	if err != nil || afterV2 != beforeV2 || afterV2.TurnJson != old.TurnJson {
		t.Fatalf("controlled v2 history read changed frozen turn: %+v %v", afterV2, err)
	}
	var projected int
	if err = store.DB.QueryRow(context.Background(), `SELECT count(*) FROM knowledge_accepted_answers_subject_v2
 WHERE issuer='rtw.identity' AND subject_id='42' AND answer_id=$1`, old.AnswerId).Scan(&projected); err != nil || projected != 1 {
		t.Fatalf("canonical sidecar projection count=%d err=%v", projected, err)
	}
}
