package metrics

import "github.com/prometheus/client_golang/prometheus"

const (
	Namespace = "article_service"
)

var (
	// ArticleTotal 文章总数统计
	ArticleTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "article_total",
			Help:      "Total number of articles processed",
		},
		[]string{"action"}, // create, update, delete
	)

	// ArticleStatusTotal 文章状态变更统计
	ArticleStatusTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "article_status_total",
			Help:      "Total number of article status changes",
		},
		[]string{"status"}, // reviewing, published, rejected
	)

	// FileUploadTotal 文件上传总数
	FileUploadTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "file_upload_total",
			Help:      "Total number of files uploaded",
		},
		[]string{"type"}, // image, markdown
	)

	// MinioRequestDuration MinIO 操作耗时
	MinioRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: Namespace,
			Name:      "minio_request_duration_seconds",
			Help:      "Latency of MinIO requests.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
		},
		[]string{"operation"}, // put, get, delete
	)

	// MinioRequestErrors MinIO 操作失败总数
	MinioRequestErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "minio_request_errors_total",
			Help:      "Total number of errors for MinIO requests",
		},
		[]string{"operation"}, // put, get, delete
	)

	// KafkaPushErrors Kafka 推送失败总数
	KafkaPushErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "kafka_push_errors_total",
			Help:      "Total number of errors for Kafka push",
		},
		[]string{"topic"},
	)
)

func init() {
	prometheus.MustRegister(ArticleTotal)
	prometheus.MustRegister(ArticleStatusTotal)
	prometheus.MustRegister(FileUploadTotal)
	prometheus.MustRegister(MinioRequestDuration)
	prometheus.MustRegister(MinioRequestErrors)
	prometheus.MustRegister(KafkaPushErrors)
}
