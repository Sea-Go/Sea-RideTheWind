package metrics

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	initOnce sync.Once

	APIRequestCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: "follow_api",
		Name:      "request_total",
		Help:      "Total number of follow api requests",
	}, []string{"route", "result"})

	APIRejectCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: "follow_api",
		Name:      "reject_total",
		Help:      "Total number of rejected follow api requests",
	}, []string{"route", "reason"})

	APIRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Subsystem: "follow_api",
		Name:      "request_duration_seconds",
		Help:      "Follow api request duration in seconds",
		Buckets:   prometheus.DefBuckets,
	}, []string{"route"})
)

func InitMetrics() {
	initOnce.Do(func() {
		prometheus.MustRegister(APIRequestCounter)
		prometheus.MustRegister(APIRejectCounter)
		prometheus.MustRegister(APIRequestDuration)
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
