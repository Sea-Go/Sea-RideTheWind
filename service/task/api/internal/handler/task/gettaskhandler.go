// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package task

import (
	"encoding/json"
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
	"sea-try-go/service/task/api/internal/logic/task"
	"sea-try-go/service/task/api/internal/svc"
	"sea-try-go/service/task/api/internal/types"
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
