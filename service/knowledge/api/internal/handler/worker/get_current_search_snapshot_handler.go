// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"fmt"
	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/model"
	"net/http"

	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/logic/worker"
	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func GetCurrentSearchSnapshotHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ModulePath
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, fmt.Errorf("%w: malformed request", model.ErrInvalid))
			return
		}

		l := worker.NewGetCurrentSearchSnapshotLogic(r.Context(), svcCtx)
		resp, err := l.GetCurrentSearchSnapshot(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
