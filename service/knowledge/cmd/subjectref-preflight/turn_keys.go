package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// encoding/json accepts repeated object keys and case-folded struct field
// aliases. A frozen v1 turn may therefore have more than one spelling for a
// single identity field even if the v1 writer accepted its final decoded value.
type turnObject uint8

const (
	turnUnknown turnObject = iota
	turnRoot
	turnRequest
	turnSubject
	turnRequestSearch
	turnSnapshot
	turnIndexes
	turnArtifact
	turnResult
	turnResultSearch
	turnPack
	turnReceipt
)

type turnKey struct {
	name  string
	child turnObject
}

const maxTurnKeyScanDepth = 128

var criticalTurnKeys = map[turnObject][]turnKey{
	turnRoot: {
		{"Request", turnRequest}, {"result", turnResult},
	},
	turnRequest: {
		{"SearchID", turnUnknown}, {"AnswerID", turnUnknown},
		{"Subject", turnSubject}, {"SessionID", turnUnknown}, {"Search", turnRequestSearch},
	},
	turnSubject: {
		{"authority_id", turnUnknown}, {"tenant_id", turnUnknown}, {"subject_id", turnUnknown},
	},
	turnRequestSearch: {
		{"Query", turnUnknown}, {"Depth", turnUnknown}, {"Intelligence", turnUnknown},
		{"Snapshot", turnSnapshot},
	},
	turnSnapshot: {
		{"module_id", turnUnknown}, {"release_id", turnUnknown},
		{"generation", turnUnknown}, {"publication_revision", turnUnknown},
		{"indexes", turnIndexes}, {"valid_revision_ids", turnUnknown},
	},
	turnResult: {
		{"answer_id", turnUnknown}, {"search", turnResultSearch},
	},
	turnResultSearch: {
		{"evidence_pack", turnPack}, {"citation_receipt", turnReceipt},
	},
	turnPack: {
		{"search_id", turnUnknown}, {"snapshot", turnSnapshot}, {"evidence", turnUnknown},
	},
	turnReceipt: {
		{"search_id", turnUnknown}, {"pack_hash", turnUnknown}, {"durable_ref", turnUnknown},
	},
	turnArtifact: {
		{"key", turnUnknown}, {"sha256", turnUnknown},
	},
}

func canonicalTurnKey(object turnObject, key string) (string, turnObject) {
	if object == turnIndexes {
		// indexes is a JSON map: unlike struct fields, lane names are exact.
		return "map:" + key, turnArtifact
	}
	for _, known := range criticalTurnKeys[object] {
		if strings.EqualFold(key, known.name) {
			return known.name, known.child
		}
	}
	return "", turnUnknown
}

func ambiguousFrozenTurnKeys(raw []byte) (bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	ambiguous, err := scanTurnValue(decoder, turnRoot, 0)
	if err != nil {
		return false, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return false, fmt.Errorf("multiple top-level turn values")
		}
		return false, err
	}
	return ambiguous, nil
}

func scanTurnValue(decoder *json.Decoder, object turnObject, depth int) (bool, error) {
	if depth >= maxTurnKeyScanDepth {
		return false, fmt.Errorf("turn key scan exceeds %d nesting levels", maxTurnKeyScanDepth)
	}
	token, err := decoder.Token()
	if err != nil {
		return false, err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return false, nil
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		ambiguous := false
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return false, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return false, fmt.Errorf("non-string turn object key")
			}
			name, child := canonicalTurnKey(object, key)
			if name != "" {
				if seen[name] {
					ambiguous = true
				}
				seen[name] = true
			}
			childAmbiguous, err := scanTurnValue(decoder, child, depth+1)
			if err != nil {
				return false, err
			}
			ambiguous = ambiguous || childAmbiguous
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
			return false, fmt.Errorf("unterminated turn object: %v", err)
		}
		return ambiguous, nil
	case '[':
		ambiguous := false
		for decoder.More() {
			childAmbiguous, err := scanTurnValue(decoder, object, depth+1)
			if err != nil {
				return false, err
			}
			ambiguous = ambiguous || childAmbiguous
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim(']') {
			return false, fmt.Errorf("unterminated turn array: %v", err)
		}
		return ambiguous, nil
	default:
		return false, fmt.Errorf("unexpected turn delimiter %q", delim)
	}
}
