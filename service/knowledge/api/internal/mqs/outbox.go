package mqs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"

	"github.com/zeromicro/go-zero/core/logx"
)

// HTTPSender implements the proposed H04 event handoff. Endpoint must be configured
// explicitly once DC implements this schema; legacy desktop endpoints are incompatible.
type HTTPSender struct {
	Endpoint string
	Token    string
	Client   *http.Client
}

func (s *HTTPSender) Send(ctx context.Context, event model.Event) (model.TechnicalReceipt, error) {
	var receipt model.TechnicalReceipt
	if s.Client == nil || s.Endpoint == "" {
		return receipt, fmt.Errorf("event sender not configured")
	}
	body, err := json.Marshal(event)
	if err != nil {
		return receipt, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Endpoint, bytes.NewReader(body))
	if err != nil {
		return receipt, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", event.EventID)
	if s.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.Token)
	}
	res, err := s.Client.Do(req)
	if err != nil {
		return receipt, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return receipt, fmt.Errorf("event receiver status %d", res.StatusCode)
	}
	err = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&receipt)
	return receipt, err
}
func Run(ctx context.Context, store *model.Store, sender model.Sender, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// A bounded batch shares the receiver and DB pool with the API.
			for i := 0; i < 16; i++ {
				attempt, cancel := context.WithTimeout(ctx, 10*time.Second)
				sent, err := store.DispatchOne(attempt, sender)
				cancel()
				if err != nil {
					logx.WithContext(ctx).Errorf("knowledge outbox delivery failed: %v", err)
					break
				}
				if !sent {
					break
				}
			}
		}
	}
}
