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

func ReadToolEvidenceHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ToolReadReq
		if err := httpx.ParsePath(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		var body struct {
			SearchID       string `json:"search_id"`
			EvidenceID     string `json:"evidence_id"`
			IdempotencyKey string `json:"idempotency_key"`
		}
		if err := decodeToolJSON(w, r, &body); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		req.SearchId, req.EvidenceId, req.IdempotencyKey = body.SearchID, body.EvidenceID, body.IdempotencyKey

		l := product.NewReadToolEvidenceLogic(r.Context(), svcCtx)
		resp, err := l.ReadToolEvidence(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.WriteJsonCtx(r.Context(), w, resp.Code, resp)
		}
	}
}
