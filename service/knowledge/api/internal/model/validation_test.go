package model

import (
	"reflect"
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/types"
)

func TestMaxSimAggregationIsExplicitAndPreserved(t *testing.T) {
	base := []types.RetrievalProfile{
		{Lane: "dense", Encoder: "dense", Tokenizer: "tokens", Space: "dense-v1", Dimensions: 2},
		{Lane: "sparse", Encoder: "sparse", Tokenizer: "tokens", Space: "sparse-v1", Dimensions: 100},
		{Lane: "multivector", Encoder: "multi", Tokenizer: "tokens", Space: "multi-v1", Dimensions: 2, Mask: "attention"},
	}
	for _, aggregation := range []string{"sum_maxsim", "mean_maxsim", "maxsim"} {
		profiles := append([]types.RetrievalProfile(nil), base...)
		profiles[2].Aggregation = aggregation
		before := append([]types.RetrievalProfile(nil), profiles...)
		if err := validateProfiles(profiles); err != nil {
			t.Fatalf("%s: %v", aggregation, err)
		}
		if !reflect.DeepEqual(profiles, before) {
			t.Fatal("aggregation was silently normalized")
		}
	}
	for _, aggregation := range []string{"", "sum", "mean", "unknown"} {
		profiles := append([]types.RetrievalProfile(nil), base...)
		profiles[2].Aggregation = aggregation
		if err := validateProfiles(profiles); err == nil {
			t.Fatalf("unknown aggregation accepted: %q", aggregation)
		}
	}
	base[2].Aggregation = "mean_maxsim"
	for _, i := range []int{0, 1} {
		profiles := append([]types.RetrievalProfile(nil), base...)
		profiles[i].Aggregation = "mean_maxsim"
		if err := validateProfiles(profiles); err == nil {
			t.Fatal("non-token aggregation accepted")
		}
	}
}

func TestTextAndLocatorValidation(t *testing.T) {
	for _, in := range []revisionInput{
		{Title: "A", Content: "A", MediaType: "application/pdf", Kind: "source", Provenance: "A"},
		{Title: "A", Content: string([]byte{0xff}), MediaType: "text/plain", Kind: "source", Provenance: "A"},
		{Title: "A", Content: "A", MediaType: "text/plain", Kind: "source"},
		{Title: "Wiki", Content: "Wiki", MediaType: "text/markdown", Kind: "wiki"},
	} {
		if validText(in) == nil {
			t.Fatal("invalid source accepted")
		}
	}
	r := types.Revision{Content: "A\r\n\r\nB\n\nC"}
	for _, locator := range []string{"paragraph:0", "paragraph:4", "paragraph:1junk", "line:1"} {
		if validLocator(r, locator) {
			t.Fatal(locator)
		}
	}
	if !validLocator(r, "paragraph:3") {
		t.Fatal("valid paragraph rejected")
	}
	for _, expiry := range []string{"", "bad", time.Now().Add(-time.Second).Format(time.RFC3339Nano)} {
		if leaseValid(expiry) {
			t.Fatal("invalid expiry", expiry)
		}
	}
}
