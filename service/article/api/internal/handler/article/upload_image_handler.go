// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package article

import (
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
	"github.com/Sea-Go/Sea-RideTheWind/service/article/api/internal/logic/article"
	"github.com/Sea-Go/Sea-RideTheWind/service/article/api/internal/svc"
)

func UploadImageHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file, header, err := r.FormFile("image")
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		defer file.Close()

		l := article.NewUploadImageLogic(r.Context(), svcCtx)
		resp, err := l.UploadImage(file, header)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
