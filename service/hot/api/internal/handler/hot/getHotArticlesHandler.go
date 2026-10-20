// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package hot

import (
	"net/http"

	"github.com/Sea-Go/Sea-RideTheWind/service/hot/api/internal/logic/hot"
	"github.com/Sea-Go/Sea-RideTheWind/service/hot/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/hot/api/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
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
