// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package comment

import (
	"net/http"

	"github.com/Sea-Go/Sea-RideTheWind/service/comment/api/internal/logic/comment"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/api/internal/types"

	"github.com/zeromicro/go-zero/rest/httpx"
)

func LikeCommentHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.LikeCommentReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := comment.NewLikeCommentLogic(r.Context(), svcCtx)
		resp, err := l.LikeComment(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
