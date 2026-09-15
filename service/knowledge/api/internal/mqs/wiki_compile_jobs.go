package mqs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// WikiCompileHTTPTransport borrows the separate DC Jobs service token. It has
// no model credential, Wiki source bytes or domain publication authority.
type WikiCompileHTTPTransport struct {
	Endpoint string // exact private /v1/jobs URL
	Token    string
	Client   *http.Client
}

func (s *WikiCompileHTTPTransport) valid() bool {
	if s == nil || s.Client == nil || s.Token == "" {
		return false
	}
	u, err := url.Parse(s.Endpoint)
	return err == nil && u != nil && (u.Scheme == "http" || u.Scheme == "https") &&
		u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == "/v1/jobs"
}

func (s *WikiCompileHTTPTransport) request(ctx context.Context, method, path, operation string,
	input any, expected ...int) ([]byte, error) {
	if !s.valid() {
		return nil, model.ErrInvalid
	}
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, s.Endpoint+path, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+s.Token)
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if operation != "" {
		request.Header.Set("Idempotency-Key", operation)
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(request.Header))
	response, err := s.Client.Do(request)
	if err != nil {
		return nil, errors.Join(model.ErrWikiCompileJobUnavailable,
			fmt.Errorf("DC Wiki Compile job HTTP: %w", err))
	}
	defer response.Body.Close()
	accepted := false
	for _, status := range expected {
		accepted = accepted || response.StatusCode == status
	}
	if !accepted {
		cause := model.ErrWikiCompileJobUnavailable
		switch response.StatusCode {
		case http.StatusBadRequest:
			cause = model.ErrInvalid
		case http.StatusUnauthorized, http.StatusForbidden:
			cause = model.ErrWikiCompileJobUnauthorized
		case http.StatusNotFound:
			cause = model.ErrNotFound
		case http.StatusConflict:
			cause = model.ErrConflict
		}
		return nil, fmt.Errorf("DC Wiki Compile job HTTP status %d: %w", response.StatusCode, cause)
	}
	bounded, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(bounded) > 64<<10 {
		return nil, fmt.Errorf("DC Wiki Compile job response: %w", model.ErrWikiCompileJobUnavailable)
	}
	return bounded, nil
}

func decodeWikiJobResponse(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing DC Wiki job response")
	}
	return nil
}

func (s *WikiCompileHTTPTransport) Submit(ctx context.Context,
	q model.WikiCompileJobSubmit) (model.WikiCompileSubmissionReceipt, error) {
	raw, err := s.request(ctx, http.MethodPost, "", q.OperationID, q, http.StatusCreated, http.StatusOK)
	if err != nil {
		return model.WikiCompileSubmissionReceipt{}, err
	}
	var receipt model.WikiCompileSubmissionReceipt
	if err := decodeWikiJobResponse(raw, &receipt); err != nil {
		return receipt, fmt.Errorf("decode DC Wiki Compile submit receipt: %w", model.ErrConflict)
	}
	return receipt, nil
}

func (s *WikiCompileHTTPTransport) Get(ctx context.Context,
	jobID string) (model.WikiCompileJobSnapshot, error) {
	if parsed, err := uuid.Parse(jobID); err != nil || parsed.String() != jobID {
		return model.WikiCompileJobSnapshot{}, model.ErrInvalid
	}
	raw, err := s.request(ctx, http.MethodGet, "/"+jobID, "", nil, http.StatusOK)
	if err != nil {
		return model.WikiCompileJobSnapshot{}, err
	}
	var job model.WikiCompileJobSnapshot
	if err := decodeWikiJobResponse(raw, &job); err != nil {
		return job, fmt.Errorf("decode DC original Wiki Compile job: %w", model.ErrConflict)
	}
	return job, nil
}

func (s *WikiCompileHTTPTransport) Cancel(ctx context.Context, jobID string,
	q model.WikiCompileJobCancel) (model.WikiCompileCancelReceipt, error) {
	if parsed, err := uuid.Parse(jobID); err != nil || parsed.String() != jobID ||
		strings.TrimSpace(q.OperationID) == "" {
		return model.WikiCompileCancelReceipt{}, model.ErrInvalid
	}
	raw, err := s.request(ctx, http.MethodPost, "/"+jobID+"/cancel", q.OperationID, q, http.StatusOK)
	if err != nil {
		return model.WikiCompileCancelReceipt{}, err
	}
	var receipt model.WikiCompileCancelReceipt
	if err := decodeWikiJobResponse(raw, &receipt); err != nil {
		return receipt, fmt.Errorf("decode DC Wiki Compile cancellation receipt: %w", model.ErrConflict)
	}
	return receipt, nil
}

// RunWikiCompileJobs is a separate, bounded candidate loop. The default
// delivery Run still handles every other domain event with its H04 sender.
func RunWikiCompileJobs(ctx context.Context, store *model.Store,
	transport *WikiCompileHTTPTransport, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for i := 0; i < 16; i++ {
				attempt, cancel := context.WithTimeout(ctx, 10*time.Second)
				sent, err := store.DispatchWikiCompileJobOnce(attempt, transport)
				cancel()
				if err != nil || !sent {
					break
				}
			}
		}
	}
}
