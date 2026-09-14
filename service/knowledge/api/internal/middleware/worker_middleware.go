package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/zeromicro/go-zero/rest/httpx"
)

type WorkerMiddleware struct{ token string }

func NewWorkerMiddleware(token string) *WorkerMiddleware { return &WorkerMiddleware{token: token} }
func (m *WorkerMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if m.token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(m.token)) != 1 {
			httpx.WriteJson(w, 401, struct {
				Code int    `json:"code"`
				Msg  string `json:"msg"`
			}{401, "worker identity required"})
			return
		}
		next(w, r)
	}
}
