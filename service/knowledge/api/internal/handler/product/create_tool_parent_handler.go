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

func CreateToolParentHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ToolParentReq
		if err := httpx.ParsePath(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		var body struct {
			ModuleID       string `json:"module_id"`
			IdempotencyKey string `json:"idempotency_key"`
		}
		if err := decodeToolJSON(w, r, &body); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		req.ModuleId, req.IdempotencyKey = body.ModuleID, body.IdempotencyKey

		l := product.NewCreateToolParentLogic(r.Context(), svcCtx)
		resp, err := l.CreateToolParent(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.WriteJsonCtx(r.Context(), w, resp.Code, resp)
		}
	}
}
