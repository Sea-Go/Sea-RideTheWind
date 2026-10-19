// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package like

import (
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/api/internal/logic/like"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/like/api/internal/types"
)

func GetUserLikeListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.GetUserLikeListReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := like.NewGetUserLikeListLogic(r.Context(), svcCtx)
		resp, err := l.GetUserLikeList(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
