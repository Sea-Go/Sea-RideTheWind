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

func GetToolParentHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ToolParentPath
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := product.NewGetToolParentLogic(r.Context(), svcCtx)
		resp, err := l.GetToolParent(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
