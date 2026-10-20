// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package like

import (
	"net/http"

	"github.com/Sea-Go/Sea-RideTheWind/service/like/api/internal/logic/like"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/api/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func GetLikeCountHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.GetLikeCountReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := like.NewGetLikeCountLogic(r.Context(), svcCtx)
		resp, err := l.GetLikeCount(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
