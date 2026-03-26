package contentsecurityservicelogic

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

	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/text/unicode/norm"
)

type SanitizeContentLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSanitizeContentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SanitizeContentLogic {
	return &SanitizeContentLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func normalizeUnicode(text string) string {
	return norm.NFC.String(text)
}

func normalizeWhitespace(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	var result strings.Builder
	result.Grow(len(text))
	inSpace := false

	for _, char := range text {
		if char == ' ' || char == '\t' || char == '\n' {
			if !inSpace {
				result.WriteRune(' ')
				inSpace = true
			}
		} else {
			result.WriteRune(char)
			inSpace = false
		}
	}

	return strings.TrimSpace(result.String())
}

func sanitizeHTML(text string, allowedTags []string) string {
	policy := bluemonday.UGCPolicy()

	if len(allowedTags) > 0 {
		policy = bluemonday.NewPolicy()
		for _, tag := range allowedTags {
			switch tag {
			case "b", "strong":
				policy.AllowAttrs("class").OnElements("b", "strong")
			case "i", "em":
				policy.AllowAttrs("class").OnElements("i", "em")
			case "u":
				policy.AllowAttrs("class").OnElements("u")
			case "s":
				policy.AllowAttrs("class").OnElements("s")
			case "sub":
				policy.AllowAttrs("class").OnElements("sub")
			case "sup":
				policy.AllowAttrs("class").OnElements("sup")
			case "blockquote":
				policy.AllowAttrs("class").OnElements("blockquote")
			case "code":
				policy.AllowAttrs("class").OnElements("code")
			case "pre":
				policy.AllowAttrs("class").OnElements("pre")
			case "ul", "ol", "li":
				policy.AllowAttrs("class").OnElements("ul", "ol", "li")
			case "dl", "dt", "dd":
				policy.AllowAttrs("class").OnElements("dl", "dt", "dd")
			case "a":
				policy.AllowAttrs("href", "target", "rel", "class").OnElements("a")
			case "img":
				policy.AllowAttrs("src", "alt", "title", "width", "height", "class").OnElements("img")
			case "p", "br":
				policy.AllowAttrs("class").OnElements("p", "br")
			}
		}
	}

	return policy.Sanitize(text)
}

func (l *SanitizeContentLogic) detectAd(text string) (bool, float32, error) {
	cfg := l.svcCtx.Config.AdDetection
	modelName := strings.TrimSpace(cfg.Model)
	if modelName == "" {
		modelName = "qwen3-max"
	}

	requestBody := map[string]interface{}{
		"model": modelName,
		"input": map[string]interface{}{
			"messages": []map[string]string{
				{
					"role": "system",
					"content": "You are an ad detection service. Return only JSON: " +
						`{"is_ad": boolean, "confidence": float}` +
						" for the given text.",
				},
				{
					"role":    "user",
					"content": text,
				},
			},
		},
		"parameters": map[string]interface{}{
			"temperature": 0.1,
			"top_p":       0.8,
		},
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		logger.LogBusinessErr(l.ctx, 500, err)
		return false, 0, err
	}

	req, err := http.NewRequestWithContext(l.ctx, http.MethodPost, cfg.ApiEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		logger.LogBusinessErr(l.ctx, 500, err)
		return false, 0, err
	}

	req.Header.Set("Authorization", "Bearer "+cfg.ApiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: time.Duration(cfg.Timeout) * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.LogBusinessErr(l.ctx, 500, err)
		return false, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.LogBusinessErr(l.ctx, 500, err)
		return false, 0, err
	}

	var result struct {
		Output struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		} `json:"output"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		logger.LogBusinessErr(l.ctx, 500, err)
		return false, 0, err
	}

	if len(result.Output.Choices) == 0 {
		err = fmt.Errorf("no choices in ad detection response: %s", string(body))
		logger.LogBusinessErr(l.ctx, 500, err)
		return false, 0, err
	}

	var adResult struct {
		IsAd       bool    `json:"is_ad"`
		Confidence float64 `json:"confidence"`
	}

	content := result.Output.Choices[0].Message.Content
	if err := json.Unmarshal([]byte(content), &adResult); err != nil {
		logger.LogBusinessErr(l.ctx, 500, err)
		isAd := strings.Contains(strings.ToLower(content), "true")
		confidence := 0.5
		if isAd {
			confidence = 0.8
		}
		return isAd, float32(confidence), nil
	}

	return adResult.IsAd, float32(adResult.Confidence), nil
}

func (l *SanitizeContentLogic) SanitizeContent(in *pb.SanitizeContentRequest) (*pb.SanitizeContentResponse, error) {
	started := time.Now()
	if in == nil {
		return &pb.SanitizeContentResponse{
			Success:      false,
			ErrorMessage: "request is nil",
			LatencyMs:    int32(time.Since(started).Milliseconds()),
		}, nil
	}

	originalText := in.GetText()
	options := in.GetOptions()
	if options == nil {
		options = &pb.SanitizeOptions{}
	}

	if originalText == "" {
		return &pb.SanitizeContentResponse{
			SanitizedText:        "",
			Success:              true,
			ObservabilityContext: cloneObservabilityContext(in.GetObservabilityContext()),
			Decision: &pb.DecisionRecord{
				Type:       "sanitize_content",
				Chosen:     "empty_text",
				Confidence: 1,
			},
			Quality: &pb.QualityValidation{
				SchemaValid:         true,
				CitationValid:       true,
				ClaimGroundingCheck: "PASS",
			},
			LatencyMs: int32(time.Since(started).Milliseconds()),
			Tokens:    &pb.TokensUsage{},
		}, nil
	}

	processedText := originalText
	if options.GetEnableUnicodeNormalization() {
		processedText = normalizeUnicode(processedText)
	}
	if options.GetEnableHtmlSanitization() {
		processedText = sanitizeHTML(processedText, l.svcCtx.Config.HtmlSanitization.AllowedTags)
	}
	if options.GetEnableWhitespaceNormalization() {
		processedText = normalizeWhitespace(processedText)
	}

	var isAd bool
	var adConfidence float32
	if options.GetEnableAdDetection() && l.svcCtx.Config.AdDetection.ApiKey != "" {
		var err error
		isAd, adConfidence, err = l.detectAd(processedText)
		if err != nil {
			logger.LogBusinessErr(l.ctx, 500, err)
			isAd = false
			adConfidence = 0
		}
		if adConfidence >= float32(l.svcCtx.Config.AdDetection.Threshold) {
			isAd = true
		}
	}

	reasonCodes := []string{"sanitized"}
	chosen := "allow"
	if isAd {
		reasonCodes = append(reasonCodes, "ad_detected")
		chosen = "reject"
	}
	if options.GetEnableHtmlSanitization() {
		reasonCodes = append(reasonCodes, "html_sanitized")
	}
	if options.GetEnableUnicodeNormalization() {
		reasonCodes = append(reasonCodes, "unicode_normalized")
	}
	if options.GetEnableWhitespaceNormalization() {
		reasonCodes = append(reasonCodes, "whitespace_normalized")
	}

	return &pb.SanitizeContentResponse{
		SanitizedText:        processedText,
		IsAd:                 isAd,
		AdConfidence:         adConfidence,
		Success:              true,
		ObservabilityContext: cloneObservabilityContext(in.GetObservabilityContext()),
		Decision: &pb.DecisionRecord{
			Type:        "sanitize_content",
			Chosen:      chosen,
			Confidence:  adConfidence,
			ReasonCodes: reasonCodes,
			Signals: map[string]string{
				"html_sanitized":         fmt.Sprintf("%t", options.GetEnableHtmlSanitization()),
				"unicode_normalized":     fmt.Sprintf("%t", options.GetEnableUnicodeNormalization()),
				"whitespace_normalized":  fmt.Sprintf("%t", options.GetEnableWhitespaceNormalization()),
				"ad_detection_requested": fmt.Sprintf("%t", options.GetEnableAdDetection()),
			},
			Constraints: &pb.DecisionConstraints{
				MaxLatencyMs: int32(time.Since(started).Milliseconds()),
			},
		},
		Quality: &pb.QualityValidation{
			SchemaValid:         true,
			CitationValid:       true,
			ClaimGroundingCheck: "PASS",
		},
		LatencyMs: int32(time.Since(started).Milliseconds()),
		Tokens:    &pb.TokensUsage{},
	}, nil
}

func cloneObservabilityContext(in *pb.ObservabilityContext) *pb.ObservabilityContext {
	if in == nil {
		return nil
	}

	out := &pb.ObservabilityContext{
		TraceId:      in.TraceId,
		SpanId:       in.SpanId,
		ParentSpanId: in.ParentSpanId,
		RequestId:    in.RequestId,
		Surface:      in.Surface,
	}
	if len(in.ExpIds) > 0 {
		out.ExpIds = make([]*pb.ExperimentInfo, 0, len(in.ExpIds))
		for _, exp := range in.ExpIds {
			if exp == nil {
				continue
			}
			out.ExpIds = append(out.ExpIds, &pb.ExperimentInfo{
				Name:    exp.Name,
				Variant: exp.Variant,
			})
		}
	}
	return out
}
