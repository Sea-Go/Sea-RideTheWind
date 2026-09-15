package subjectrefturn

import "testing"

func TestPreflightAndProductReadKeyModes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		raw       string
		preflight bool
		productV2 bool
	}{
		{"critical_case_alias", `{"Request":{"Subject":{"subject_id":"42","SUBJECT_ID":"42"}}}`, true, true},
		{"critical_unicode_alias", `{"Request":{"Subject":{"subject_id":"42","\u0073ubject_id":"42"}}}`, true, true},
		{"unknown_exact_duplicate", `{"Request":{},"metadata":{"custom":1,"custom":2}}`, false, true},
		{"unknown_case_distinct", `{"Request":{},"metadata":{"custom":1,"CUSTOM":2}}`, false, false},
		{"map_lane_case_distinct", `{"Request":{"Search":{"Snapshot":{"indexes":{"dense":{},"Dense":{}}}}}}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			preflight, err := AmbiguousFrozenTurnKeys([]byte(tc.raw), ScanOptions{})
			if err != nil || preflight != tc.preflight {
				t.Fatalf("preflight ambiguity=%t want=%t err=%v", preflight, tc.preflight, err)
			}
			productV2, err := AmbiguousFrozenTurnKeys([]byte(tc.raw), ScanOptions{RejectAnyExactDuplicate: true})
			if err != nil || productV2 != tc.productV2 {
				t.Fatalf("product v2 ambiguity=%t want=%t err=%v", productV2, tc.productV2, err)
			}
		})
	}
}
