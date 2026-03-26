package favorite

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"sea-try-go/service/common/logger"
	favoritecommon "sea-try-go/service/favorite/common"
)

func extractUserID(ctx context.Context) (int64, error) {
	raw := ctx.Value("userId")
	if raw == nil {
		return 0, fmt.Errorf("ctx userId is nil")
	}

	switch v := raw.(type) {
	case json.Number:
		return v.Int64()
	case string:
		return strconv.ParseInt(v, 10, 64)
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case float64:
		return int64(v), nil
	default:
		return 0, fmt.Errorf("ctx userId has unexpected type %T", raw)
	}
}

func userLogOption(userID int64) logger.LogOption {
	return logger.WithUserID(strconv.FormatInt(userID, 10))
}

func articleLogOption(articleID string) logger.LogOption {
	return logger.WithArticleID(strings.TrimSpace(articleID))
}

func codeFromRPCError(err error) int {
	return favoritecommon.BizCodeFromError(err)
}
