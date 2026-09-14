// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package product

import (
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
	"sea-try-go/service/knowledge/api/internal/logic/product"
	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"
)

func CreateToolSearchHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ToolSearchReq
		if err := httpx.ParsePath(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		var body struct {
			Query            string `json:"query"`
			Depth            string `json:"depth"`
			Intelligence     string `json:"intelligence"`
			ContinueSearchID string `json:"continue_search_id"`
			ReadCalls        int    `json:"read_calls"`
			QuoteRunes       int    `json:"quote_runes"`
			IdempotencyKey   string `json:"idempotency_key"`
		}
		if err := decodeToolJSON(w, r, &body); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		req.Query, req.Depth, req.Intelligence = body.Query, body.Depth, body.Intelligence
		req.ContinueSearchId, req.ReadCalls, req.QuoteRunes, req.IdempotencyKey =
			body.ContinueSearchID, body.ReadCalls, body.QuoteRunes, body.IdempotencyKey

		l := product.NewCreateToolSearchLogic(r.Context(), svcCtx)
		resp, err := l.CreateToolSearch(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.WriteJsonCtx(r.Context(), w, resp.Code, resp)
		}
	}
}
