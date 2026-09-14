package user

import (
	"context"

	"sea-try-go/service/user/user/api/internal/identity"
	"sea-try-go/service/user/user/api/internal/types"
	"sea-try-go/service/user/user/rpc/pb"
)

func currentUserID(ctx context.Context) (int64, error) {
	return identity.ClaimedUID(ctx)
}

func avatarHistoryItemFromPB(item *pb.AvatarHistoryItem) types.AvatarHistoryItem {
	if item == nil {
		return types.AvatarHistoryItem{}
	}

	return types.AvatarHistoryItem{
		Id:          item.Id,
		AvatarUrl:   item.AvatarUrl,
		ContentType: item.ContentType,
		SizeBytes:   item.SizeBytes,
		IsCurrent:   item.IsCurrent,
		CreateTime:  item.CreateTime,
	}
}
