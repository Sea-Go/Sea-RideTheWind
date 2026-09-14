package model

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

func TestFavoriteFactUsesExactDecimalSnowflakeIDs(t *testing.T) {
	for _, item := range []FavoriteItem{
		{FavoriteId: 501, FolderId: 41, UserId: 1001, TargetType: "article", TargetId: "article-low"},
		{FavoriteId: 9007199254740995, FolderId: 9007199254740993,
			UserId: 1001, TargetType: "article", TargetId: "article-high"},
	} {
		for _, op := range []struct {
			version   int64
			operation string
		}{{1, "assert"}, {2, "retract"}} {
			row, err := favoriteOutbox(item, op.version, op.operation, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			var event favoriteEvent
			if err := json.Unmarshal([]byte(row.Payload), &event); err != nil {
				t.Fatal(err)
			}
			if event.Payload.FavoriteID != strconv.FormatInt(item.FavoriteId, 10) ||
				event.Payload.FolderID != strconv.FormatInt(item.FolderId, 10) ||
				event.AggregateID != event.Payload.FavoriteID || event.Payload.Operation != op.operation {
				t.Fatalf("inexact favorite event: %+v", event)
			}
		}
	}
}
