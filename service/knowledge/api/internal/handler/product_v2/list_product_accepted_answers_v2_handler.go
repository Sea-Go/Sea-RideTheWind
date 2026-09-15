// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package product_v2

import (
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
	"sea-try-go/service/knowledge/api/internal/logic/product_v2"
	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"
)

func ListProductAcceptedAnswersV2Handler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ProductAcceptedAnswersReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := product_v2.NewListProductAcceptedAnswersV2Logic(r.Context(), svcCtx)
		resp, err := l.ListProductAcceptedAnswersV2(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
