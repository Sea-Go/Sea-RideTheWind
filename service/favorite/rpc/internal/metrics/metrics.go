package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/Sea-Go/Sea-RideTheWind/service/favorite/rpc/internal/config"
)

var (
	// RPC 请求计数
	FavoriteRequestCounterMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "",
		Subsystem: "favorite",
		Name:      "request_total",
		Help:      "",
	}, []string{"module", "action", "result"})

	// RPC 耗时累加（秒）
	FavoriteRequestSecondsCounterMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "",
		Subsystem: "favorite",
		Name:      "request_seconds_counter",
		Help:      "",
	}, []string{"module", "action"})

	// 收藏业务操作计数
	FavoriteOpsCounterMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "",
		Subsystem: "favorite",
		Name:      "ops_total",
		Help:      "",
	}, []string{"module", "action", "result"})

	// 数据库错误计数
	FavoriteDBErrorCounterMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "",
		Subsystem: "favorite",
		Name:      "db_error_total",
		Help:      "",
	}, []string{"module", "action", "type"})
)

func InitMetrics(cfg *config.Config) {
	prometheus.Unregister(collectors.NewGoCollector())
	prometheus.Unregister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	prometheus.MustRegister(FavoriteRequestCounterMetric)
	prometheus.MustRegister(FavoriteRequestSecondsCounterMetric)
	prometheus.MustRegister(FavoriteOpsCounterMetric)
	prometheus.MustRegister(FavoriteDBErrorCounterMetric)
}

func ObserveRPC(module, action string, started time.Time, err error) {
	result := "success"
	if err != nil {
		result = "fail"
	}

	FavoriteRequestCounterMetric.WithLabelValues(module, action, result).Inc()
	FavoriteRequestSecondsCounterMetric.WithLabelValues(module, action).Add(time.Since(started).Seconds())
}

func ObserveOp(module, action, result string) {
	FavoriteOpsCounterMetric.WithLabelValues(module, action, result).Inc()
}

func ObserveDBError(module, action, typ string) {
	FavoriteDBErrorCounterMetric.WithLabelValues(module, action, typ).Inc()
}
