package follow

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"sea-try-go/service/common/logger"
	followcommon "sea-try-go/service/follow/common"
)

func extractUserID(ctx context.Context) (int64, error) {
	raw := ctx.Value("userId")
	if raw == nil {
		return 0, fmt.Errorf("ctx userId is nil")
	}

	switch value := raw.(type) {
	case json.Number:
		return value.Int64()
	case string:
		return strconv.ParseInt(value, 10, 64)
	case int64:
		return value, nil
	case int:
		return int64(value), nil
	case float64:
		return int64(value), nil
	default:
		return 0, fmt.Errorf("ctx userId has unexpected type %T", raw)
	}
}

func userLogOption(userID int64) logger.LogOption {
	return logger.WithUserID(strconv.FormatInt(userID, 10))
}

func codeFromRPCError(err error) int {
	return followcommon.BizCodeFromError(err)
}

func resolveUserID(currentUserID, requestedUserID int64) int64 {
	if requestedUserID > 0 {
		return requestedUserID
	}
	return currentUserID
}
