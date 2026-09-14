// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"fmt"
	"net/http"
	"sea-try-go/service/knowledge/api/internal/model"

	"sea-try-go/service/knowledge/api/internal/logic/worker"
	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/rest/httpx"
)

func GetBuildHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.BuildPath
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, fmt.Errorf("%w: malformed request", model.ErrInvalid))
			return
		}

		l := worker.NewGetBuildLogic(r.Context(), svcCtx)
		resp, err := l.GetBuild(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
