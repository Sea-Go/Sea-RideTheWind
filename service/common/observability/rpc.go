package observability

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/zeromicro/go-zero/zrpc"
)

func DisableNativeRpcTimeout(conf *zrpc.RpcServerConf) time.Duration {
	timeout := TimeoutFromMillis(conf.Timeout)
	conf.Timeout = 0
	return timeout
}

func NewUnaryServerInterceptor(timeout, slowThreshold time.Duration) grpc.UnaryServerInterceptor {
	if slowThreshold <= 0 {
		slowThreshold = DefaultSlowThreshold
	}

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		if timeout <= 0 {
			resp, err := handler(ctx, req)
			markRPCResult(ctx, info.FullMethod, err, time.Since(start), slowThreshold)
			return resp, err
		}

		timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		var resp any
		var err error
		var lock sync.Mutex
		done := make(chan struct{})
		panicChan := make(chan any, 1)

		go func() {
			defer func() {
				if p := recover(); p != nil {
					panicChan <- fmt.Sprintf("%+v\n\n%s", p, strings.TrimSpace(string(debug.Stack())))
				}
			}()
			lock.Lock()
			defer lock.Unlock()
			resp, err = handler(timeoutCtx, req)
			close(done)
		}()

		select {
		case p := <-panicChan:
			MarkSystemError(ctx, fmt.Errorf("%v", p), time.Since(start), rpcAttrs(info.FullMethod, codes.Internal)...)
			panic(p)
		case <-done:
			lock.Lock()
			defer lock.Unlock()
			markRPCResult(timeoutCtx, info.FullMethod, err, time.Since(start), slowThreshold)
			return resp, err
		case <-timeoutCtx.Done():
			err := status.Error(codes.DeadlineExceeded, timeoutCtx.Err().Error())
			MarkTimeout(ctx, err, time.Since(start), rpcAttrs(info.FullMethod, codes.DeadlineExceeded)...)
			return nil, err
		}
	}
}

func markRPCResult(ctx context.Context, method string, err error, duration, slowThreshold time.Duration) {
	code := codes.OK
	if err != nil {
		if st, ok := status.FromError(err); ok {
			code = st.Code()
		} else {
			code = codes.Unknown
		}
	}
	attrs := rpcAttrs(method, code)
	if err == nil {
		MarkDurationAndSlow(ctx, duration, slowThreshold, attrs...)
		return
	}
	if IsTimeoutError(err) {
		MarkTimeout(ctx, err, duration, attrs...)
		return
	}
	if isBusinessGRPCCode(code) {
		MarkBusinessError(ctx, int(code), err.Error(), duration, attrs...)
		return
	}
	MarkSystemError(ctx, err, duration, attrs...)
}

func isBusinessGRPCCode(code codes.Code) bool {
	switch code {
	case codes.InvalidArgument,
		codes.NotFound,
		codes.AlreadyExists,
		codes.PermissionDenied,
		codes.Unauthenticated,
		codes.FailedPrecondition,
		codes.OutOfRange:
		return true
	default:
		return false
	}
}

func rpcAttrs(method string, code codes.Code) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("rpc.system", "grpc"),
		attribute.String("rpc.method", method),
		attribute.Int("rpc.grpc.status_code", int(code)),
	}
}
