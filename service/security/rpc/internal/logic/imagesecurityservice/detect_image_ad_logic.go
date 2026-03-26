package imagesecurityservicelogic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"sea-try-go/service/common/logger"
	"sea-try-go/service/security/rpc/internal/svc"
	pb "sea-try-go/service/security/rpc/pb/sea-try-go/service/security/rpc/pb"
)

type DetectImageAdLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDetectImageAdLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DetectImageAdLogic {
	return &DetectImageAdLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

type dashScopeResponse struct {
	Output struct {
		Choices []struct {
			Message struct {
				Content interface{} `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	} `json:"output"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
	RequestID string `json:"request_id"`
}

type adDetectionResult struct {
	IsAd          bool    `json:"is_ad"`
	AdConfidence  float64 `json:"ad_confidence"`
	ExtractedText string  `json:"extracted_text"`
	Success       bool    `json:"success"`
	ErrorMessage  string  `json:"error_message"`
}

func (l *DetectImageAdLogic) callAIModel(imageInput string, confidenceThreshold float64, enableTextExtraction bool) (*adDetectionResult, error) {
	config := l.svcCtx.Config.AIModel
	if strings.TrimSpace(config.ModelEndpoint) == "" {
		return nil, fmt.Errorf("image moderation model endpoint is not configured")
	}
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, fmt.Errorf("image moderation api key is not configured")
	}

	timeoutSeconds := config.Timeout
	if timeoutSeconds <= 0 {
		timeoutSeconds = 300
	}

	requestPayload := map[string]interface{}{
		"model": "qwen-vl-max",
		"input": map[string]interface{}{
			"messages": []map[string]interface{}{
				{
					"role": "user",
					"content": []map[string]interface{}{
						{"image": imageInput},
						{"text": buildImagePrompt(confidenceThreshold, enableTextExtraction)},
					},
				},
			},
		},
		"parameters": map[string]interface{}{
			"temperature": 0.1,
			"top_p":       0.8,
		},
	}

	requestBody, err := json.Marshal(requestPayload)
	if err != nil {
		return nil, fmt.Errorf("marshal image moderation request: %w", err)
	}

	apiCtx, cancel := context.WithTimeout(l.ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(apiCtx, http.MethodPost, config.ModelEndpoint, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("create image moderation request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+config.APIKey)

	client := &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call image moderation endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read image moderation response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image moderation endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	var parsed dashScopeResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal image moderation response: %w", err)
	}
	if len(parsed.Output.Choices) == 0 {
		return nil, fmt.Errorf("image moderation response did not contain any choices")
	}

	contentText := dashScopeContentToString(parsed.Output.Choices[0].Message.Content)
	jsonText := extractJSON(contentText)

	var result adDetectionResult
	if err := json.Unmarshal([]byte(jsonText), &result); err != nil {
		logger.LogInfo(l.ctx, "image moderation returned non-json payload, using fallback parser",
			logger.WithUserID(fmt.Sprintf("content=%s", contentText)))
		result = parseAdResultFallback(contentText)
	}

	result.Success = true
	return &result, nil
}

func dashScopeContentToString(content interface{}) string {
	switch value := content.(type) {
	case string:
		return value
	case []interface{}:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			if textItem, ok := item.(map[string]interface{}); ok {
				if text, ok := textItem["text"].(string); ok {
					parts = append(parts, text)
					continue
				}
			}
			parts = append(parts, fmt.Sprintf("%v", item))
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	default:
		return fmt.Sprintf("%v", value)
	}
}

func extractJSON(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return trimmed
	}

	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```json")
		trimmed = strings.TrimPrefix(trimmed, "```JSON")
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimSuffix(trimmed, "```")
		trimmed = strings.TrimSpace(trimmed)
	}

	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		return strings.TrimSpace(trimmed[start : end+1])
	}

	return trimmed
}

func parseAdResultFallback(content string) adDetectionResult {
	normalized := strings.ToLower(content)
	result := adDetectionResult{
		IsAd:          containsAny(normalized, []string{`"is_ad": true`, `"is_ad":true`, "advertisement", "promotion", "promo", "marketing", "buy now", "discount", "coupon", "telegram", "whatsapp", "wechat", "vx"}),
		AdConfidence:  0.5,
		ExtractedText: content,
	}

	if containsAny(normalized, []string{"high confidence", `"ad_confidence": 0.9`, `"ad_confidence":0.9`, `"ad_confidence": 1`, `"ad_confidence":1`}) {
		result.AdConfidence = 0.9
	} else if containsAny(normalized, []string{"low confidence", `"ad_confidence": 0.2`, `"ad_confidence":0.2`, `"ad_confidence": 0.3`, `"ad_confidence":0.3`}) {
		result.AdConfidence = 0.3
	}

	return result
}

func buildImagePrompt(confidenceThreshold float64, enableTextExtraction bool) string {
	prompt := fmt.Sprintf(
		"Review the image and decide whether it is an advertisement. Use %.2f as the confidence threshold. Return JSON only: {\"is_ad\": true/false, \"ad_confidence\": 0.0-1.0, \"extracted_text\": \"...\"}.",
		confidenceThreshold,
	)
	if enableTextExtraction {
		prompt += " Extract visible text when it helps the decision."
	} else {
		prompt += " Keep extracted_text brief if text is not important."
	}
	return prompt
}

func containsAny(text string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func shouldDowngradeOfficialMediaPoster(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" {
		return false
	}

	officialSignals := []string{
		"official poster",
		"official teaser",
		"official trailer",
		"anime",
		"movie",
		"tv series",
		"copyright",
		"all rights reserved",
	}
	adSignals := []string{
		"buy",
		"sale",
		"discount",
		"coupon",
		"shop",
		"wechat",
		"telegram",
		"whatsapp",
		"price",
		"order now",
	}

	return containsAny(normalized, officialSignals) && !containsAny(normalized, adSignals)
}

func (l *DetectImageAdLogic) DetectImageAd(in *pb.DetectImageAdRequest) (*pb.DetectImageAdResponse, error) {
	if in == nil {
		return &pb.DetectImageAdResponse{
			Success:      false,
			ErrorMessage: "request is nil",
		}, nil
	}

	if strings.TrimSpace(in.GetImageUrl()) == "" && strings.TrimSpace(in.GetImageBase64()) == "" {
		return &pb.DetectImageAdResponse{
			Success:      false,
			ErrorMessage: "image_url or image_base64 is required",
		}, nil
	}

	confidenceThreshold := l.svcCtx.Config.AIModel.ConfidenceThreshold
	if confidenceThreshold <= 0 {
		confidenceThreshold = 0.7
	}
	enableTextExtraction := true

	if in.GetOptions() != nil {
		if in.GetOptions().GetConfidenceThreshold() > 0 {
			confidenceThreshold = float64(in.GetOptions().GetConfidenceThreshold())
		}
		enableTextExtraction = in.GetOptions().GetEnableTextExtraction()
	}

	imageInput := in.GetImageUrl()
	if strings.TrimSpace(in.GetImageBase64()) != "" {
		imageInput = in.GetImageBase64()
	}

	if strings.TrimSpace(l.svcCtx.Config.AIModel.ModelEndpoint) == "" || strings.TrimSpace(l.svcCtx.Config.AIModel.APIKey) == "" {
		return &pb.DetectImageAdResponse{
			Success:      true,
			ErrorMessage: "image moderation skipped because the image model is not fully configured",
		}, nil
	}

	modelResp, err := l.callAIModel(imageInput, confidenceThreshold, enableTextExtraction)
	if err != nil {
		logger.LogBusinessErr(l.ctx, 500, err)
		return &pb.DetectImageAdResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("image moderation service error: %v", err),
		}, nil
	}

	if modelResp.IsAd && shouldDowngradeOfficialMediaPoster(modelResp.ExtractedText) {
		logger.LogInfo(l.ctx, "downgrading likely official media poster false positive",
			logger.WithUserID(fmt.Sprintf("extracted_text=%s", modelResp.ExtractedText)))
		modelResp.IsAd = false
		modelResp.AdConfidence = 0.15
	}

	response := &pb.DetectImageAdResponse{
		IsAd:          modelResp.IsAd,
		AdConfidence:  float32(modelResp.AdConfidence),
		ExtractedText: modelResp.ExtractedText,
		Success:       true,
	}

	logger.LogInfo(l.ctx, "image ad detection completed",
		logger.WithUserID(fmt.Sprintf("is_ad=%t confidence=%.2f", response.IsAd, response.AdConfidence)))

	return response, nil
}
