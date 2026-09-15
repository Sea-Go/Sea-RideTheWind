package product

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"
	"sea-try-go/service/user/user/identity"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

var errSearchUpstream = errors.New("search summary upstream unavailable")

// The ordered fields and exact json.Marshal bytes are shared with BTW's H02
// validator. No client field participates in this server-issued scope.
type searchScope struct {
	Audience               string                   `json:"aud"`
	Subject                types.AcceptedSubjectRef `json:"subject_ref"`
	SessionID              string                   `json:"session_id"`
	SearchID               string                   `json:"search_id"`
	AnswerID               string                   `json:"answer_id"`
	Snapshot               types.SearchSnapshot     `json:"snapshot"`
	AllowPartial           bool                     `json:"allow_partial"`
	AllowLowerIntelligence bool                     `json:"allow_lower_intelligence"`
	RequestHash            string                   `json:"request_hash"`
	IssuedAtUnix           int64                    `json:"issued_at_unix"`
	ExpiresAtUnix          int64                    `json:"expires_at_unix"`
}

// v2 has its own audience and exact ordered two-field identity wire. The old
// searchScope type, HMAC key, and published v1 payload bytes remain unchanged.
type searchScopeV2 struct {
	Audience               string                `json:"aud"`
	Subject                identity.SubjectRefV2 `json:"subject_ref"`
	SessionID              string                `json:"session_id"`
	SearchID               string                `json:"search_id"`
	AnswerID               string                `json:"answer_id"`
	Snapshot               types.SearchSnapshot  `json:"snapshot"`
	AllowPartial           bool                  `json:"allow_partial"`
	AllowLowerIntelligence bool                  `json:"allow_lower_intelligence"`
	RequestHash            string                `json:"request_hash"`
	IssuedAtUnix           int64                 `json:"issued_at_unix"`
	ExpiresAtUnix          int64                 `json:"expires_at_unix"`
}

func signSearchScopeV2(op model.ProductSearchOperation, subject identity.SubjectRefV2,
	key string, allowPartial, allowLower bool, now time.Time) (string, error) {
	if len(key) < 32 || !identity.ValidSubjectRefV2(subject) ||
		op.Subject.AuthorityId != subject.Issuer || op.Subject.TenantId != identity.PlatformTenantID ||
		op.Subject.SubjectId != subject.SubjectID {
		return "", errSearchUpstream
	}
	hash, err := op.Search.Hash()
	if err != nil || hash != op.RequestHash || op.Snapshot.ModuleId != op.Search.ModuleID {
		return "", errSearchUpstream
	}
	payload, err := json.Marshal(searchScopeV2{
		Audience: "btw.search.summary.v2", Subject: subject, SessionID: op.SessionID,
		SearchID: op.SearchID, AnswerID: op.AnswerID, Snapshot: op.Snapshot,
		AllowPartial: allowPartial, AllowLowerIntelligence: allowLower,
		RequestHash: op.RequestHash, IssuedAtUnix: now.Unix(), ExpiresAtUnix: now.Add(120 * time.Second).Unix(),
	})
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func signSearchScope(op model.ProductSearchOperation, key string, allowPartial, allowLower bool, now time.Time) (string, error) {
	if len(key) < 32 {
		return "", errSearchUpstream
	}
	payload, err := json.Marshal(searchScope{
		Audience: "btw.search.summary.v1", Subject: op.Subject, SessionID: op.SessionID,
		SearchID: op.SearchID, AnswerID: op.AnswerID, Snapshot: op.Snapshot,
		AllowPartial: allowPartial, AllowLowerIntelligence: allowLower,
		RequestHash: op.RequestHash, IssuedAtUnix: now.Unix(), ExpiresAtUnix: now.Add(120 * time.Second).Unix(),
	})
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func callBTWSummary(ctx context.Context, service *svc.ServiceContext, op model.ProductSearchOperation) (types.ProductSearchResult, error) {
	var empty types.ProductSearchResult
	if service.Config.SearchSummary.Endpoint == "" || service.SearchHTTP == nil {
		return empty, errSearchUpstream
	}
	budget := service.Config.SearchSummary.FastTimeoutMillis
	if op.Search.Depth == "detailed" {
		budget = service.Config.SearchSummary.DetailedTimeoutMillis
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(budget)*time.Millisecond)
	defer cancel()
	version, err := service.Store.ProductSearchScopeVersion(callCtx, op)
	if err != nil {
		return empty, err
	}
	var header string
	if version == "v2" {
		if err = service.Store.RequireSearchScopeVersions(); err != nil {
			return empty, err
		}
		var subject identity.SubjectRefV2
		subject, err = issuedSearchSubjectV2(callCtx, service, op.Subject)
		if err == nil {
			header, err = signSearchScopeV2(op, subject, service.Config.SearchSummary.ScopeKey,
				service.Config.SearchSummary.AllowPartial, service.Config.SearchSummary.AllowLowerIntelligence, time.Now())
		}
	} else {
		header, err = signSearchScope(op, service.Config.SearchSummary.ScopeKey,
			service.Config.SearchSummary.AllowPartial, service.Config.SearchSummary.AllowLowerIntelligence, time.Now())
	}
	if err != nil {
		return empty, err
	}
	body, err := json.Marshal(op.Search)
	if err != nil {
		return empty, err
	}
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, service.Config.SearchSummary.Endpoint, bytes.NewReader(body))
	if err != nil {
		return empty, errSearchUpstream
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Sea-Search-Scope", header)
	otel.GetTextMapPropagator().Inject(callCtx, propagation.HeaderCarrier(req.Header))
	response, err := service.SearchHTTP.Do(req)
	if err != nil {
		return empty, errSearchUpstream
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > 1<<20 {
		return empty, errSearchUpstream
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return empty, errSearchUpstream
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var result types.ProductSearchResult
	if err = decoder.Decode(&result); err != nil {
		return empty, errSearchUpstream
	}
	var tail any
	if decoder.Decode(&tail) != io.EOF {
		return empty, errSearchUpstream
	}
	if result.SearchId != op.SearchID || result.AnswerId != op.AnswerID ||
		(result.Status != "succeeded" && result.Status != "insufficient") {
		return empty, errSearchUpstream
	}
	return result, nil
}
