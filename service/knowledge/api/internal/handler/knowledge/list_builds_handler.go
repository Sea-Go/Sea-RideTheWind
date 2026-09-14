// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"fmt"
	"net/http"

	"sea-try-go/service/knowledge/api/internal/logic/knowledge"
	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/rest/httpx"
)

func ListBuildsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ModulePageReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, fmt.Errorf("%w: %v", model.ErrInvalid, err))
			return
		}

		l := knowledge.NewListBuildsLogic(r.Context(), svcCtx)
		resp, err := l.ListBuilds(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
