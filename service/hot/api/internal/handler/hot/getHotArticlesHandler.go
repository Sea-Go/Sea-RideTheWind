// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package hot

import (
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
	"sea-try-go/service/hot/api/internal/logic/hot"
	"sea-try-go/service/hot/api/internal/svc"
	"sea-try-go/service/hot/api/internal/types"
)

func GetHotArticlesHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.HotArticlesReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := hot.NewGetHotArticlesLogic(r.Context(), svcCtx)
		resp, err := l.GetHotArticles(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
