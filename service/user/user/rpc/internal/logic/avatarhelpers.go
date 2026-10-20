package logic

import (
	"strings"
	"time"

	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/internal/model"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/rpc/pb"
)

func avatarHistoryItemFromModel(item *model.UserAvatarHistory) *pb.AvatarHistoryItem {
	if item == nil {
		return nil
	}

	createTime := ""
	if !item.CreateTime.IsZero() {
		createTime = item.CreateTime.Format(time.RFC3339)
	}

	return &pb.AvatarHistoryItem{
		Id:          item.HistoryID,
		AvatarUrl:   item.AvatarURL,
		ContentType: item.ContentType,
		SizeBytes:   item.SizeBytes,
		IsCurrent:   item.IsCurrent,
		CreateTime:  createTime,
	}
}

func normalizedAvatarURL(value string) string {
	return strings.TrimSpace(value)
}
