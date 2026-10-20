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

func ReportCommentHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ReportCommentReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := comment.NewReportCommentLogic(r.Context(), svcCtx)
		resp, err := l.ReportComment(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
