// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package task

import (
	"encoding/json"
	"net/http"

	"github.com/Sea-Go/Sea-RideTheWind/service/task/api/internal/logic/task"
	"github.com/Sea-Go/Sea-RideTheWind/service/task/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/task/api/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func GetTaskHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.GetTaskReq
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := task.NewGetTaskLogic(r.Context(), svcCtx)
		resp, err := l.GetTask(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
