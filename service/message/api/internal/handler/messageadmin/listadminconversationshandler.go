// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package messageadmin

import (
	"net/http"

	"github.com/Sea-Go/Sea-RideTheWind/service/message/api/internal/logic/messageadmin"
	"github.com/Sea-Go/Sea-RideTheWind/service/message/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/message/api/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func ListAdminConversationsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.PageReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := messageadmin.NewListAdminConversationsLogic(r.Context(), svcCtx)
		resp, err := l.ListAdminConversations(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
