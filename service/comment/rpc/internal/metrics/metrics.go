package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/internal/config"
)

var (
	CommentRequestCounterMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: "comment",
		Name:      "request_total",
		Help:      "Total number of comment rpc requests",
	}, []string{"module", "action", "result"})

	CommentRequestSecondsCounterMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: "comment",
		Name:      "request_seconds_counter",
		Help:      "Accumulated request duration seconds for comment rpc",
	}, []string{"module", "action"})

	CommentListSizeGaugeMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: "comment",
		Name:      "list_size",
		Help:      "Last returned size of comment list",
	}, []string{"module", "action"})

	CommentPostgresErrorCounterMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: "comment",
		Name:      "postgres_error_total",
		Help:      "Total number of postgres errors in comment service",
	}, []string{"module", "action", "type"})

	CommentRedisErrorCounterMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: "comment",
		Name:      "redis_error_total",
		Help:      "Total number of redis errors in comment service",
	}, []string{"module", "action", "type"})
)

func InitMetrics(cfg *config.Config) {
	prometheus.Unregister(collectors.NewGoCollector())
	prometheus.Unregister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	prometheus.MustRegister(CommentRequestCounterMetric)
	prometheus.MustRegister(CommentRequestSecondsCounterMetric)
	prometheus.MustRegister(CommentListSizeGaugeMetric)
	prometheus.MustRegister(CommentPostgresErrorCounterMetric)
	prometheus.MustRegister(CommentRedisErrorCounterMetric)
}
