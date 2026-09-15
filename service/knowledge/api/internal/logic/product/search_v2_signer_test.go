package product

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/types"
	"sea-try-go/service/user/user/identity"
)

const signerTestKey = "test-only-v2-scope-key-more-than-32-bytes"

func signedPayload(t *testing.T, header string) []byte {
	t.Helper()
	parts := strings.Split(header, ".")
	if len(parts) != 2 {
		t.Fatal("signed scope has no canonical two-part wire")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || base64.RawURLEncoding.EncodeToString(payload) != parts[0] {
		t.Fatal("noncanonical scope encoding")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || base64.RawURLEncoding.EncodeToString(sig) != parts[1] {
		t.Fatal("noncanonical HMAC encoding")
	}
	mac := hmac.New(sha256.New, []byte(signerTestKey))
	_, _ = mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		t.Fatal("HMAC was not calculated over transmitted payload bytes")
	}
	return payload
}

func TestV2SignerUsesDistinctCanonicalSummaryAndToolsWire(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	legacy := types.AcceptedSubjectRef{AuthorityId: identity.AuthorityID,
		TenantId: identity.PlatformTenantID, SubjectId: "42"}
	v2 := identity.SubjectRefV2{Issuer: identity.AuthorityID, SubjectID: "42"}
	search := model.ProductSearchInput{ModuleID: "module_test", Query: "find Evidence", Depth: "fast", Intelligence: "low"}
	hash, err := search.Hash()
	if err != nil {
		t.Fatal(err)
	}
	op := model.ProductSearchOperation{Subject: legacy, SessionID: "same-session",
		Search: search, Snapshot: types.SearchSnapshot{ModuleId: search.ModuleID},
		SearchID: "search_test", AnswerID: "answer_test", RequestHash: hash}
	old, err := signSearchScope(op, signerTestKey, false, false, now)
	if err != nil {
		t.Fatal(err)
	}
	newHeader, err := signSearchScopeV2(op, v2, signerTestKey, false, false, now)
	if err != nil || newHeader == old {
		t.Fatalf("v2 summary signer reused v1 wire: %v", err)
	}
	newPayload := signedPayload(t, newHeader)
	var summary searchScopeV2
	if err := json.Unmarshal(newPayload, &summary); err != nil {
		t.Fatal(err)
	}
	canonical, _ := json.Marshal(summary)
	if !bytes.Equal(newPayload, canonical) || summary.Audience != "btw.search.summary.v2" ||
		summary.Subject != v2 || bytes.Contains(newPayload, []byte(`"tenant_id"`)) ||
		bytes.Contains(newPayload, []byte(`"authority_id"`)) {
		t.Fatal("v2 summary scope added a compatibility identity field or changed canonical bytes")
	}
	parent := model.ToolParent{Subject: legacy, SessionID: "same-session", OperationID: "toolop_test",
		ModuleID: "module_test", Snapshot: types.SearchSnapshot{ModuleId: "module_test"},
		BudgetRef: "budget_test", SnapshotRef: "snapshot_test", ExpiresAt: now.Add(10 * time.Minute)}
	input := model.ToolSearchInput{Query: "find Evidence", Depth: "fast", Intelligence: "low",
		ReadCalls: 8, QuoteRunes: 8192}
	inputHash, err := input.Hash()
	if err != nil {
		t.Fatal(err)
	}
	child := model.ToolSearch{OperationID: parent.OperationID, SearchID: "toolsearch_test",
		Input: input, RequestHash: inputHash,
		ReservedReads: 8, ReservedRunes: 8192}
	body, toolsHeader, err := signToolsSearchV2(parent, child, v2, signerTestKey, false, false, now)
	if err != nil || body.SearchID != child.SearchID {
		t.Fatalf("v2 Tool signer failed: %v %+v", err, body)
	}
	toolsPayload := signedPayload(t, toolsHeader)
	var tools toolsSearchScopeV2
	if err := json.Unmarshal(toolsPayload, &tools); err != nil {
		t.Fatal(err)
	}
	canonical, _ = json.Marshal(tools)
	if !bytes.Equal(toolsPayload, canonical) || tools.Audience != "btw.search.tools.v2" ||
		tools.Subject != v2 || bytes.Contains(toolsPayload, []byte(`"tenant_id"`)) ||
		bytes.Contains(toolsPayload, []byte(`"authority_id"`)) ||
		bytes.Equal(toolsPayload, newPayload) {
		t.Fatal("v2 Tool audience or two-field subject is not distinct and canonical")
	}
	oldAgain, err := signSearchScope(op, signerTestKey, false, false, now)
	if err != nil || oldAgain != old {
		t.Fatal("v2 signer mutated the v1 scope bytes")
	}
}

func TestV2SignerRejectsUnissuedUIDAndWrongLegacySlotBeforeHMAC(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	legacy := types.AcceptedSubjectRef{AuthorityId: identity.AuthorityID,
		TenantId: identity.PlatformTenantID, SubjectId: "42"}
	search := model.ProductSearchInput{ModuleID: "module_test", Query: "find Evidence", Depth: "fast", Intelligence: "low"}
	hash, err := search.Hash()
	if err != nil {
		t.Fatal(err)
	}
	op := model.ProductSearchOperation{Subject: legacy, Search: search,
		Snapshot: types.SearchSnapshot{ModuleId: search.ModuleID}, RequestHash: hash}
	parent := model.ToolParent{Subject: legacy, OperationID: "toolop_test", ModuleID: "module_test",
		Snapshot: types.SearchSnapshot{ModuleId: "module_test"}, ExpiresAt: now.Add(time.Minute)}
	input := model.ToolSearchInput{Query: "find Evidence", Depth: "fast", Intelligence: "low",
		ReadCalls: 8, QuoteRunes: 8192}
	inputHash, err := input.Hash()
	if err != nil {
		t.Fatal(err)
	}
	child := model.ToolSearch{OperationID: parent.OperationID, SearchID: "search_test",
		Input: input, RequestHash: inputHash, ReservedReads: 8, ReservedRunes: 8192}
	for _, bad := range []identity.SubjectRefV2{
		{Issuer: "other.identity", SubjectID: "42"}, {Issuer: identity.AuthorityID, SubjectID: "0"},
		{Issuer: identity.AuthorityID, SubjectID: "042"},
		{Issuer: identity.AuthorityID, SubjectID: "9223372036854775808"},
		{Issuer: identity.AuthorityID, SubjectID: "43"},
	} {
		if _, err := signSearchScopeV2(op, bad, signerTestKey, false, false, now); err == nil {
			t.Fatalf("Summary signed invalid v2 subject %+v", bad)
		}
		if _, _, err := signToolsSearchV2(parent, child, bad, signerTestKey, false, false, now); err == nil {
			t.Fatalf("Tools signed invalid v2 subject %+v", bad)
		}
	}
	legacy.TenantId = "organization-x"
	op.Subject = legacy
	parent.Subject = legacy
	good := identity.SubjectRefV2{Issuer: identity.AuthorityID, SubjectID: "42"}
	if _, err := signSearchScopeV2(op, good, signerTestKey, false, false, now); err == nil {
		t.Fatal("Summary signed nonplatform legacy compatibility slot")
	}
	if _, _, err := signToolsSearchV2(parent, child, good, signerTestKey, false, false, now); err == nil {
		t.Fatal("Tools signed nonplatform legacy compatibility slot")
	}
}
