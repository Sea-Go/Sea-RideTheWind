package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	favoritecommon "github.com/Sea-Go/Sea-RideTheWind/service/favorite/common"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

type CheckBlacklistMiddleware struct {
	Redis *redis.Redis
}

func NewCheckBlacklistMiddleware(r *redis.Redis) *CheckBlacklistMiddleware {
	return &CheckBlacklistMiddleware{Redis: r}
}

func (m *CheckBlacklistMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			next(w, r)
			return
		}

		token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		if token == "" {
			next(w, r)
			return
		}

		blackListKey := fmt.Sprintf("user:jwt_blacklist:%s", token)
		exists, err := m.Redis.ExistsCtx(r.Context(), blackListKey)
		if err == nil && exists {
			w.Header().Set("Content-Type", "application/json;charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"code":%d,"msg":"%s"}`,
				favoritecommon.ErrorUnauthorized,
				favoritecommon.GetErrMsg(favoritecommon.ErrorUnauthorized),
			)))
			return
		}

		ctx := context.WithValue(r.Context(), "jwt_token", token)
		next(w, r.WithContext(ctx))
	}
}
