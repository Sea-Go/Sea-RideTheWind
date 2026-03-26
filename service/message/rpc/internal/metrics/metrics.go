package metrics

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	initOnce sync.Once

	RPCRequestCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: "message_rpc",
		Name:      "request_total",
		Help:      "Total number of message rpc requests",
	}, []string{"method", "result"})

	RPCRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Subsystem: "message_rpc",
		Name:      "request_duration_seconds",
		Help:      "Message rpc request duration in seconds",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method"})

	NotificationActionCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: "message_rpc",
		Name:      "notification_action_total",
		Help:      "Total number of notification actions",
	}, []string{"kind", "result"})

	ChatActionCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: "message_rpc",
		Name:      "chat_action_total",
		Help:      "Total number of chat actions",
	}, []string{"action", "result"})

	DBErrorCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: "message_rpc",
		Name:      "db_error_total",
		Help:      "Total number of message rpc database errors",
	}, []string{"action", "type"})
)

func InitMetrics() {
	initOnce.Do(func() {
		prometheus.MustRegister(RPCRequestCounter)
		prometheus.MustRegister(RPCRequestDuration)
		prometheus.MustRegister(NotificationActionCounter)
		prometheus.MustRegister(ChatActionCounter)
		prometheus.MustRegister(DBErrorCounter)
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

func ObserveNotification(kind string, err error) {
	result := "success"
	if err != nil {
		result = "fail"
	}

	NotificationActionCounter.WithLabelValues(kind, result).Inc()
}

func ObserveChat(action string, err error) {
	result := "success"
	if err != nil {
		result = "fail"
	}

	ChatActionCounter.WithLabelValues(action, result).Inc()
}

func ObserveDBError(action, typ string) {
	DBErrorCounter.WithLabelValues(action, typ).Inc()
}
