package admin

import (
	"net/http"

	"sea-try-go/service/common/response"
	"sea-try-go/service/user/admin/api/internal/logic/admin"
	"sea-try-go/service/user/admin/api/internal/svc"
	"sea-try-go/service/user/admin/api/internal/types"
	"sea-try-go/service/user/common/errmsg"

	"github.com/zeromicro/go-zero/rest/httpx"
)

func CreateinviteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.CreateInviteReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := admin.NewCreateinviteLogic(r.Context(), svcCtx)
		resp, code := l.Createinvite(&req)
		httpx.OkJson(w, &response.Response{
			Code: code,
			Msg:  errmsg.GetErrMsg(code),
			Data: resp,
		})
	}
}
