package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func likeMessageHash(p *LikeProcessPayload) (string, error) {
	payload, err := json.Marshal(struct {
		UserID     int64  `json:"user_id"`
		TargetType string `json:"target_type"`
		TargetID   string `json:"target_id"`
		AuthorID   int64  `json:"author_id"`
		Action     int32  `json:"action"`
		OccurredAt int64  `json:"occurred_at"`
		IsFirst    bool   `json:"is_first"`
	}{
		UserID: p.Record.UserID, TargetType: p.Record.TargetType, TargetID: p.Record.TargetID,
		AuthorID: p.Record.AuthorID, Action: p.Record.State, OccurredAt: p.OccurredAt, IsFirst: p.IsFirst,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}
