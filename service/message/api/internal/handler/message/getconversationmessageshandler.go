// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package message

import (
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
	"github.com/Sea-Go/Sea-RideTheWind/service/message/api/internal/logic/message"
	"github.com/Sea-Go/Sea-RideTheWind/service/message/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/message/api/internal/types"
)

func GetConversationMessagesHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ConversationMessagesReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := message.NewGetConversationMessagesLogic(r.Context(), svcCtx)
		resp, err := l.GetConversationMessages(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
