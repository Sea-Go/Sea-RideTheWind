// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package knowledge

import (
	"github.com/zeromicro/go-zero/rest/httpx"
	"net/http"

	"sea-try-go/service/knowledge/api/internal/logic/knowledge"
	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"
)

func JudgeWikiFactHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.JudgeWikiFactReq
		if err := parseJudgeWikiFact(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := knowledge.NewJudgeWikiFactLogic(r.Context(), svcCtx)
		resp, err := l.JudgeWikiFact(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
