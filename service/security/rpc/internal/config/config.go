package config

import (
	"github.com/zeromicro/go-zero/zrpc"
)

type AIModelConfig struct {
	// ModelEndpoint AI模型服务的HTTP端点
	ModelEndpoint string `json:"modelEndpoint"`
	// APIKey 用于认证的API密钥
	APIKey string `json:"apiKey"`
	// Timeout 请求超时时间（秒）
	Timeout int `json:"timeout"`
	// ConfidenceThreshold 默认置信度阈值
	ConfidenceThreshold float64 `json:"confidenceThreshold"`
}

type Config struct {
	zrpc.RpcServerConf
	// AIModel AI模型配置
	AIModel AIModelConfig `json:"aiModel"`

	AdDetection struct {
		ApiEndpoint string  `json:"apiEndpoint"`
		ApiKey      string  `json:"apiKey"`
		Threshold   float64 `json:"threshold"`
		Timeout     int     `json:"timeout"`
		Model       string  `json:"model"`
	}
	HtmlSanitization struct {
		AllowedTags []string `json:"allowedTags"`
	}
	//Cache cache.CacheConf
}
