package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/zeromicro/go-zero/core/logx"

	"github.com/Sea-Go/Sea-RideTheWind/service/hot/rpc/internal/config"
)

func InitMetrics(cfg *config.Config) {
	prometheus.Unregister(collectors.NewGoCollector())
	prometheus.Unregister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		addr := ":9091"
		logx.Infof("Prometheus metrics server listening on %s", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			logx.Errorf("Failed to start metrics server: %v", err)
		}
	}()
}
