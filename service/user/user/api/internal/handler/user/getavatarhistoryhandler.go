// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package user

import (
	"net/http"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/response"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/api/internal/logic/user"
	"github.com/Sea-Go/Sea-RideTheWind/service/user/user/api/internal/svc"

	"github.com/zeromicro/go-zero/rest/httpx"
)

func GetavatarhistoryHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := user.NewGetavatarhistoryLogic(r.Context(), svcCtx)
		resp, code := l.Getavatarhistory()
		httpx.OkJson(w, &response.Response{
			Code: code,
			Msg:  errmsg.GetErrMsg(code),
			Data: resp,
		})
	}
}
