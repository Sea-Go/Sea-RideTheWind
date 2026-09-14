// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package product

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
	"sea-try-go/service/knowledge/api/internal/logic/product"
	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"
)

func CreateProductSearchHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ProductSearchReq
		media, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if mediaErr != nil || media != "application/json" {
			httpx.ErrorCtx(r.Context(), w, fmt.Errorf("%w: application/json required", model.ErrInvalid))
			return
		}
		if err := httpx.ParsePath(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, fmt.Errorf("%w: malformed session path", model.ErrInvalid))
			return
		}
		var body struct {
			ModuleID       string `json:"module_id"`
			Query          string `json:"query"`
			Depth          string `json:"depth"`
			Intelligence   string `json:"intelligence"`
			IdempotencyKey string `json:"idempotency_key"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			httpx.ErrorCtx(r.Context(), w, fmt.Errorf("%w: malformed search request", model.ErrInvalid))
			return
		}
		var tail any
		if decoder.Decode(&tail) != io.EOF {
			httpx.ErrorCtx(r.Context(), w, fmt.Errorf("%w: trailing search request", model.ErrInvalid))
			return
		}
		req.ModuleId, req.Query, req.Depth, req.Intelligence, req.IdempotencyKey =
			body.ModuleID, body.Query, body.Depth, body.Intelligence, body.IdempotencyKey

		l := product.NewCreateProductSearchLogic(r.Context(), svcCtx)
		resp, err := l.CreateProductSearch(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.WriteJsonCtx(r.Context(), w, resp.Code, resp)
		}
	}
}
