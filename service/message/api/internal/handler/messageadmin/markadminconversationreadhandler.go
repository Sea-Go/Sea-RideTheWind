// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package messageadmin

import (
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
	"sea-try-go/service/message/api/internal/logic/messageadmin"
	"sea-try-go/service/message/api/internal/svc"
	"sea-try-go/service/message/api/internal/types"
)

func MarkAdminConversationReadHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.MarkConversationReadReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := messageadmin.NewMarkAdminConversationReadLogic(r.Context(), svcCtx)
		resp, err := l.MarkAdminConversationRead(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
