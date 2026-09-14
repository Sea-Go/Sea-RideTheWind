package handler

import (
	"context"
	"errors"
	"net/http"

	"sea-try-go/service/common/response"
	"sea-try-go/service/knowledge/api/internal/model"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// ConfigureResponses configures go-zero's shared envelope once during process startup.
func ConfigureResponses() {
	httpx.SetErrorHandlerCtx(func(ctx context.Context, err error) (int, any) {
		status := http.StatusInternalServerError
		msg := "knowledge service unavailable"
		switch {
		case errors.Is(err, model.ErrInvalid):
			status = 400
			msg = err.Error()
		case errors.Is(err, model.ErrNotFound):
			status = 404
			msg = err.Error()
		case errors.Is(err, model.ErrConflict):
			status = 409
			msg = err.Error()
		case errors.Is(err, model.ErrArtifactUnavailable):
			status = 503
			msg = model.ErrArtifactUnavailable.Error()
		case errors.Is(err, model.ErrUnavailable):
			status = 410
			msg = err.Error()
		case errors.Is(err, context.DeadlineExceeded):
			status = 504
		case errors.Is(err, context.Canceled):
			status = 499
		default:
			logx.WithContext(ctx).Errorf("knowledge request failed: %v", err)
		}
		return status, response.Response{Code: status, Msg: msg}
	})
}
