package comment

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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
