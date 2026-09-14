package model

import (
	"testing"

	"sea-try-go/service/knowledge/api/internal/object"
	"sea-try-go/service/knowledge/api/internal/types"
)

func TestCitationSourceKeepsCRLFBytesAndUnicodeRuneSpan(t *testing.T) {
	content := "第一段。\r\n\r\n  海洋  \r\n 星球  "
	block, start, end, ok := citationParagraph(content, "paragraph:2")
	if !ok || content[start:end] != "  海洋  \r\n 星球  " || citationNormalize(block) != "海洋\n星球" {
		t.Fatalf("original byte mapping drifted: %q %d %d %v", block, start, end, ok)
	}
	quote := "洋\n星"
	revision := types.Revision{RevisionId: "rev_unicode", EntityId: "source_unicode", Kind: "source",
		ObjectKey: object.Key(object.Hash([]byte(content))), ContentHash: object.Hash([]byte(content)), Content: content}
	chunk := types.CitationChunk{RevisionId: revision.RevisionId, ContentId: revision.EntityId,
		SourceKind: revision.Kind, Original: types.CitationObject{Key: revision.ObjectKey, Sha256: revision.ContentHash},
		Location: types.CitationLocation{Locator: "paragraph:2", OriginalByteStart: start, OriginalByteEnd: end,
			NormalizedRuneStart: 1, NormalizedRuneEnd: 4}, Text: quote, TextHash: object.Hash([]byte(quote))}
	if err := sourceFromManifest(chunk, revision); err != nil {
		t.Fatal(err)
	}
	chunk.Location.OriginalByteEnd--
	if err := sourceFromManifest(chunk, revision); err != ErrArtifactUnavailable {
		t.Fatalf("shifted original byte boundary accepted: %v", err)
	}
}
