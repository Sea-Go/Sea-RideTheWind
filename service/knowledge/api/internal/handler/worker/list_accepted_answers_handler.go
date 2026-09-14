// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"fmt"
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
	"sea-try-go/service/knowledge/api/internal/logic/worker"
	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"
)

func ListAcceptedAnswersHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ListAcceptedAnswersReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, fmt.Errorf("%w: malformed request", model.ErrInvalid))
			return
		}

		l := worker.NewListAcceptedAnswersLogic(r.Context(), svcCtx)
		resp, err := l.ListAcceptedAnswers(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
