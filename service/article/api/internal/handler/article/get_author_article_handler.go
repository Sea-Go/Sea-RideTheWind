// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package article

import (
	"net/http"

	"github.com/Sea-Go/Sea-RideTheWind/service/article/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/response"

	"github.com/zeromicro/go-zero/rest/httpx"
	"github.com/Sea-Go/Sea-RideTheWind/service/article/api/internal/logic/article"
	"github.com/Sea-Go/Sea-RideTheWind/service/article/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/article/api/internal/types"
)

func GetAuthorArticleHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.GetArticleReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := article.NewGetAuthorArticleLogic(r.Context(), svcCtx)
		resp, code := l.GetAuthorArticle(&req)
		httpx.OkJson(w, &response.Response{Code: code, Msg: errmsg.GetErrMsg(code), Data: resp})
	}
}
