package model

import (
	"testing"
	"time"

	"sea-try-go/service/knowledge/api/internal/types"
)

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
