// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package product_v2

import (
	"net/http"

	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/logic/product_v2"
	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func GetProductAcceptedAnswerV2Handler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ProductAcceptedAnswerReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := product_v2.NewGetProductAcceptedAnswerV2Logic(r.Context(), svcCtx)
		resp, err := l.GetProductAcceptedAnswerV2(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
