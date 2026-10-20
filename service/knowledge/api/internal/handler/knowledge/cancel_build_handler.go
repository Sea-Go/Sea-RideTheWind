// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"fmt"
	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/model"
	"net/http"

	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/logic/knowledge"
	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/rest/httpx"
)

func CancelBuildHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.CancelBuildReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, fmt.Errorf("%w: malformed request", model.ErrInvalid))
			return
		}

		l := knowledge.NewCancelBuildLogic(r.Context(), svcCtx)
		resp, err := l.CancelBuild(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
