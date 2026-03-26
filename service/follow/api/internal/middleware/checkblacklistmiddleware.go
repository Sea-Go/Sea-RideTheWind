// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	followcommon "sea-try-go/service/follow/common"

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

		blacklistKey := fmt.Sprintf("user:jwt_blacklist:%s", token)
		exists, err := m.Redis.ExistsCtx(r.Context(), blacklistKey)
		if err == nil && exists {
			w.Header().Set("Content-Type", "application/json;charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(fmt.Sprintf(
				`{"code":%d,"msg":"%s"}`,
				followcommon.ErrorUnauthorized,
				followcommon.GetErrMsg(followcommon.ErrorUnauthorized),
			)))
			return
		}

		ctx := context.WithValue(r.Context(), "jwt_token", token)
		next(w, r.WithContext(ctx))
	}
}
