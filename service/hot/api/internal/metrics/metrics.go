package metrics

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	initOnce sync.Once

	APIRequestCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: "hot_api",
		Name:      "request_total",
		Help:      "Total number of hot api requests",
	}, []string{"route", "result"})

	APIRejectCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: "hot_api",
		Name:      "reject_total",
		Help:      "Total number of rejected hot api requests",
	}, []string{"route", "reason"})

	APIRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Subsystem: "hot_api",
		Name:      "request_duration_seconds",
		Help:      "Hot api request duration in seconds",
		Buckets:   prometheus.DefBuckets,
	}, []string{"route"})

	APIRPCErrorCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: "hot_api",
		Name:      "rpc_error_total",
		Help:      "Total number of hot api rpc errors",
	}, []string{"callee"})

	APIArticleSkipCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: "hot_api",
		Name:      "article_skip_total",
		Help:      "Total number of skipped hot articles during hydration",
	}, []string{"reason"})
)

func InitMetrics() {
	initOnce.Do(func() {
		prometheus.MustRegister(APIRequestCounter)
		prometheus.MustRegister(APIRejectCounter)
		prometheus.MustRegister(APIRequestDuration)
		prometheus.MustRegister(APIRPCErrorCounter)
		prometheus.MustRegister(APIArticleSkipCounter)
	})
}

func ObserveRequest(route string, started time.Time, err error) {
	result := "success"
	if err != nil {
		result = "fail"
	}

	APIRequestCounter.WithLabelValues(route, result).Inc()
	APIRequestDuration.WithLabelValues(route).Observe(time.Since(started).Seconds())
}

func ObserveReject(route, reason string) {
	APIRejectCounter.WithLabelValues(route, reason).Inc()
}

func ObserveRPCError(callee string) {
	APIRPCErrorCounter.WithLabelValues(callee).Inc()
}

func ObserveArticleSkip(reason string) {
	APIArticleSkipCounter.WithLabelValues(reason).Inc()
}
