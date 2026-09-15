package main

import "sea-try-go/service/knowledge/internal/subjectrefturn"

const maxTurnKeyScanDepth = subjectrefturn.MaxTurnKeyScanDepth

// Preflight retains its frozen stage-1 critical-key scan. The product v2
// reader separately enables exact-key duplicate rejection on this shared seam.
func ambiguousFrozenTurnKeys(raw []byte) (bool, error) {
	return subjectrefturn.AmbiguousFrozenTurnKeys(raw, subjectrefturn.ScanOptions{})
}
