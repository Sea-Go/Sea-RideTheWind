// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package admin

import (
	"net/http"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/response"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/api/internal/logic/admin"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/admin/api/internal/types"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/errmsg"

	"github.com/zeromicro/go-zero/rest/httpx"
)

func LoginHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.LoginReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := admin.NewLoginLogic(r.Context(), svcCtx)
		resp, code := l.Login(&req)
		httpx.OkJson(w, &response.Response{
			Code: code,
			Msg:  errmsg.GetErrMsg(code),
			Data: resp,
		})
	}
}
