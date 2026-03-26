package metrics

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	initOnce sync.Once

	RPCRequestCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: "follow_rpc",
		Name:      "request_total",
		Help:      "Total number of follow rpc requests",
	}, []string{"method", "result"})

	RPCRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Subsystem: "follow_rpc",
		Name:      "request_duration_seconds",
		Help:      "Follow rpc request duration in seconds",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method"})

	RelationActionCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: "follow_rpc",
		Name:      "relation_action_total",
		Help:      "Total number of relation actions",
	}, []string{"action", "result"})

	DBErrorCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: "follow_rpc",
		Name:      "db_error_total",
		Help:      "Total number of follow rpc database errors",
	}, []string{"action", "type"})

	RecommendationSizeGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Subsystem: "follow_rpc",
		Name:      "recommendation_size",
		Help:      "Last generated recommendation size",
	})
)

func InitMetrics() {
	initOnce.Do(func() {
		prometheus.MustRegister(RPCRequestCounter)
		prometheus.MustRegister(RPCRequestDuration)
		prometheus.MustRegister(RelationActionCounter)
		prometheus.MustRegister(DBErrorCounter)
		prometheus.MustRegister(RecommendationSizeGauge)
	})
}

func ObserveRPC(method string, started time.Time, err error) {
	result := "success"
	if err != nil {
		result = "fail"
	}

	RPCRequestCounter.WithLabelValues(method, result).Inc()
	RPCRequestDuration.WithLabelValues(method).Observe(time.Since(started).Seconds())
}

func ObserveRelation(action string, err error) {
	result := "success"
	if err != nil {
		result = "fail"
	}

	RelationActionCounter.WithLabelValues(action, result).Inc()
}

func ObserveDBError(action, typ string) {
	DBErrorCounter.WithLabelValues(action, typ).Inc()
}

func ObserveRecommendationSize(size int) {
	RecommendationSizeGauge.Set(float64(size))
}
