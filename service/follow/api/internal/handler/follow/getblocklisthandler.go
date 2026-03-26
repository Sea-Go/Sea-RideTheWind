// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package follow

import (
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
	"sea-try-go/service/follow/api/internal/logic/follow"
	"sea-try-go/service/follow/api/internal/svc"
	"sea-try-go/service/follow/api/internal/types"
)

func GetBlockListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.FollowListReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := follow.NewGetBlockListLogic(r.Context(), svcCtx)
		resp, err := l.GetBlockList(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
