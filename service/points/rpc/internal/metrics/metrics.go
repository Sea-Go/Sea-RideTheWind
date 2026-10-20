package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/Sea-Go/Sea-RideTheWind/service/points/rpc/internal/config"
)

var (
	// RPC 请求计数
	PointsRequestCounterMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "",
		Subsystem: "points",
		Name:      "request_total",
		Help:      "",
	}, []string{"module", "action", "result"})

	// RPC 耗时累加（秒）
	PointsRequestSecondsCounterMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "",
		Subsystem: "points",
		Name:      "request_seconds_counter",
		Help:      "",
	}, []string{"module", "action"})

	// 积分操作计数
	PointsOpsCounterMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "",
		Subsystem: "points",
		Name:      "ops_total",
		Help:      "",
	}, []string{"module", "action", "result"})

	// Kafka 消费错误计数
	PointsKafkaErrorCounterMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "",
		Subsystem: "points",
		Name:      "kafka_error_total",
		Help:      "",
	}, []string{"module", "action", "type"})
)

func InitMetrics(cfg *config.Config) {
	prometheus.Unregister(collectors.NewGoCollector())
	prometheus.Unregister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	prometheus.Register(PointsRequestCounterMetric)
	prometheus.Register(PointsRequestSecondsCounterMetric)
	prometheus.Register(PointsOpsCounterMetric)
	prometheus.Register(PointsKafkaErrorCounterMetric)
}
