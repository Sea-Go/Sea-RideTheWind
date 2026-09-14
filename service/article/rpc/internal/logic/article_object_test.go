package logic

import (
	"strings"
	"testing"
)

func TestArticleEditKeepsOldObjectAndArticleID(t *testing.T) {
	const prefix, articleID = "articles/", "article-77"
	legacy := prefix + articleID + ".md"
	first := articleMarkdownObjectName(prefix, articleID, "# First")
	second := articleMarkdownObjectName(prefix, articleID, "# Second")
	if first == legacy || second == legacy || first == second ||
		first != articleMarkdownObjectName(prefix, articleID, "# First") ||
		!strings.HasPrefix(first, prefix+articleID+"-") ||
		!strings.HasPrefix(second, prefix+articleID+"-") {
		t.Fatalf("edit object keys are not stable immutable descendants of article ID: %q %q", first, second)
	}
}
