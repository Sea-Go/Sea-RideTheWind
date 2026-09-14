// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package product

import (
	"fmt"
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
	"sea-try-go/service/knowledge/api/internal/logic/product"
	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"
)

func GetProductAnswerCitationStatesHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ProductAcceptedAnswerReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, fmt.Errorf("%w: malformed request", model.ErrInvalid))
			return
		}

		l := product.NewGetProductAnswerCitationStatesLogic(r.Context(), svcCtx)
		resp, err := l.GetProductAnswerCitationStates(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
