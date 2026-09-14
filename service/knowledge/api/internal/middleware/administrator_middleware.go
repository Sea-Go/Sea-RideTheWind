package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
)

type AdministratorMiddleware struct{ ids map[string]bool }

func NewAdministratorMiddleware(ids []string) *AdministratorMiddleware {
	allowed := map[string]bool{}
	for _, id := range ids {
		allowed[id] = true
	}
	return &AdministratorMiddleware{ids: allowed}
}
func (m *AdministratorMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var id string
		switch v := r.Context().Value("userId").(type) {
		case string:
			id = v
		case json.Number:
			id = v.String()
		}
		if id == "" || !m.ids[id] {
			httpx.WriteJson(w, http.StatusForbidden, struct {
				Code int    `json:"code"`
				Msg  string `json:"msg"`
			}{403, "knowledge administrator required"})
			return
		}
		next(w, r)
	}
}
func Actor(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
