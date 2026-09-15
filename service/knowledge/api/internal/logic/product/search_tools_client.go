package product

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

var errToolsUpstream = errors.New("search Tools upstream unavailable")

// Field order is part of the H02 Tools request-hash contract. All values are
// selected by RTW after authenticated parent/child reservation.
type toolsSearchBody struct {
	ModuleID     string `json:"module_id"`
	Query        string `json:"query"`
	Depth        string `json:"depth"`
	Intelligence string `json:"intelligence"`
	SearchID     string `json:"search_id"`
	Limits       struct {
		ReadCalls  int `json:"read_calls"`
		QuoteRunes int `json:"quote_runes"`
	} `json:"limits"`
}

type toolsSearchScope struct {
	Audience               string                   `json:"aud"`
	Subject                types.AcceptedSubjectRef `json:"subject_ref"`
	SessionID              string                   `json:"session_id"`
	OperationID            string                   `json:"operation_id"`
	BudgetRef              string                   `json:"budget_ref"`
	SearchID               string                   `json:"search_id"`
	SnapshotRef            string                   `json:"snapshot_ref"`
	Snapshot               types.SearchSnapshot     `json:"snapshot"`
	AllowPartial           bool                     `json:"allow_partial"`
	AllowLowerIntelligence bool                     `json:"allow_lower_intelligence"`
	RequestHash            string                   `json:"request_hash"`
	IssuedAtUnix           int64                    `json:"issued_at_unix"`
	ExpiresAtUnix          int64                    `json:"expires_at_unix"`
}

type toolsSearchScopeV2 struct {
	Audience               string                `json:"aud"`
	Subject                identity.SubjectRefV2 `json:"subject_ref"`
	SessionID              string                `json:"session_id"`
	OperationID            string                `json:"operation_id"`
	BudgetRef              string                `json:"budget_ref"`
	SearchID               string                `json:"search_id"`
	SnapshotRef            string                `json:"snapshot_ref"`
	Snapshot               types.SearchSnapshot  `json:"snapshot"`
	AllowPartial           bool                  `json:"allow_partial"`
	AllowLowerIntelligence bool                  `json:"allow_lower_intelligence"`
	RequestHash            string                `json:"request_hash"`
	IssuedAtUnix           int64                 `json:"issued_at_unix"`
	ExpiresAtUnix          int64                 `json:"expires_at_unix"`
}

func signToolsSearchV2(parent model.ToolParent, child model.ToolSearch, subject identity.SubjectRefV2,
	key string, allowPartial, allowLower bool, now time.Time) (toolsSearchBody, string, error) {
	var body toolsSearchBody
	if len(key) < 32 || child.OperationID != parent.OperationID || child.SearchID == "" ||
		!now.Before(parent.ExpiresAt) || !identity.ValidSubjectRefV2(subject) ||
		parent.Subject.AuthorityId != subject.Issuer ||
		parent.Subject.TenantId != identity.PlatformTenantID || parent.Subject.SubjectId != subject.SubjectID {
		return body, "", errToolsUpstream
	}
	inputHash, err := child.Input.Hash()
	if err != nil || inputHash != child.RequestHash || parent.Snapshot.ModuleId != parent.ModuleID ||
		child.ReservedReads != child.Input.ReadCalls || child.ReservedRunes != child.Input.QuoteRunes {
		return body, "", errToolsUpstream
	}
	body = toolsSearchBody{ModuleID: parent.ModuleID, Query: child.Input.Query,
		Depth: child.Input.Depth, Intelligence: child.Input.Intelligence, SearchID: child.SearchID}
	body.Limits.ReadCalls = child.ReservedReads
	body.Limits.QuoteRunes = child.ReservedRunes
	raw, err := json.Marshal(body)
	if err != nil {
		return body, "", err
	}
	hash := sha256.Sum256(raw)
	expires := now.Add(120 * time.Second)
	if parent.ExpiresAt.Before(expires) {
		expires = parent.ExpiresAt
	}
	scope := toolsSearchScopeV2{Audience: "btw.search.tools.v2", Subject: subject,
		SessionID: parent.SessionID, OperationID: parent.OperationID, BudgetRef: parent.BudgetRef,
		SearchID: child.SearchID, SnapshotRef: parent.SnapshotRef, Snapshot: parent.Snapshot,
		AllowPartial: allowPartial, AllowLowerIntelligence: allowLower,
		RequestHash: hex.EncodeToString(hash[:]), IssuedAtUnix: now.Unix(), ExpiresAtUnix: expires.Unix()}
	payload, err := json.Marshal(scope)
	if err != nil {
		return body, "", err
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write(payload)
	return body, base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func signToolsSearch(parent model.ToolParent, child model.ToolSearch, key string,
	allowPartial, allowLower bool, now time.Time) (toolsSearchBody, string, error) {
	var body toolsSearchBody
	if len(key) < 32 || child.OperationID != parent.OperationID || child.SearchID == "" ||
		!now.Before(parent.ExpiresAt) {
		return body, "", errToolsUpstream
	}
	body = toolsSearchBody{ModuleID: parent.ModuleID, Query: child.Input.Query,
		Depth: child.Input.Depth, Intelligence: child.Input.Intelligence, SearchID: child.SearchID}
	body.Limits.ReadCalls = child.ReservedReads
	body.Limits.QuoteRunes = child.ReservedRunes
	raw, err := json.Marshal(body)
	if err != nil {
		return body, "", err
	}
	hash := sha256.Sum256(raw)
	expires := now.Add(120 * time.Second)
	if parent.ExpiresAt.Before(expires) {
		expires = parent.ExpiresAt
	}
	scope := toolsSearchScope{Audience: "btw.search.tools.v1", Subject: parent.Subject,
		SessionID: parent.SessionID, OperationID: parent.OperationID, BudgetRef: parent.BudgetRef,
		SearchID: child.SearchID, SnapshotRef: parent.SnapshotRef, Snapshot: parent.Snapshot,
		AllowPartial: allowPartial, AllowLowerIntelligence: allowLower,
		RequestHash: hex.EncodeToString(hash[:]), IssuedAtUnix: now.Unix(), ExpiresAtUnix: expires.Unix()}
	payload, err := json.Marshal(scope)
	if err != nil {
		return body, "", err
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write(payload)
	return body, base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func callBTWToolsSearch(ctx context.Context, service *svc.ServiceContext,
	parent model.ToolParent, child model.ToolSearch) (types.ToolSearchResult, error) {
	var empty types.ToolSearchResult
	if service.SearchHTTP == nil || service.Config.SearchTools.Endpoint == "" {
		return empty, errToolsUpstream
	}
	now := time.Now()
	version, err := service.Store.ToolParentScopeVersion(ctx, parent.Subject, parent.SessionID, parent.OperationID)
	if err != nil {
		return empty, err
	}
	var body toolsSearchBody
	var signed string
	if version == "v2" {
		if err = service.Store.RequireSearchScopeVersions(); err != nil {
			return empty, err
		}
		var subject identity.SubjectRefV2
		subject, err = issuedSearchSubjectV2(ctx, service, parent.Subject)
		if err == nil {
			body, signed, err = signToolsSearchV2(parent, child, subject, service.Config.SearchTools.ScopeKey,
				service.Config.SearchTools.AllowPartial, service.Config.SearchTools.AllowLowerIntelligence, now)
		}
	} else {
		body, signed, err = signToolsSearch(parent, child, service.Config.SearchTools.ScopeKey,
			service.Config.SearchTools.AllowPartial, service.Config.SearchTools.AllowLowerIntelligence, now)
	}
	if err != nil {
		return empty, err
	}
	budget := service.Config.SearchTools.FastTimeoutMillis
	if child.Input.Depth == "detailed" {
		budget = service.Config.SearchTools.DetailedTimeoutMillis
	}
	remaining := time.Until(parent.ExpiresAt)
	if remaining <= 0 {
		return empty, errToolsUpstream
	}
	if time.Duration(budget)*time.Millisecond > remaining {
		budget = int(remaining.Milliseconds())
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(budget)*time.Millisecond)
	defer cancel()
	raw, err := json.Marshal(body)
	if err != nil {
		return empty, err
	}
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, service.Config.SearchTools.Endpoint, bytes.NewReader(raw))
	if err != nil {
		return empty, errToolsUpstream
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Sea-Search-Tools-Scope", signed)
	otel.GetTextMapPropagator().Inject(callCtx, propagation.HeaderCarrier(req.Header))
	response, err := service.SearchHTTP.Do(req)
	if err != nil {
		return empty, errToolsUpstream
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > 1<<20 {
		return empty, errToolsUpstream
	}
	responseRaw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(responseRaw) > 1<<20 {
		return empty, errToolsUpstream
	}
	decoder := json.NewDecoder(bytes.NewReader(responseRaw))
	decoder.DisallowUnknownFields()
	var result types.ToolSearchResult
	if err = decoder.Decode(&result); err != nil {
		return empty, errToolsUpstream
	}
	var tail any
	if decoder.Decode(&tail) != io.EOF {
		return empty, errToolsUpstream
	}
	if result.SearchId != child.SearchID || result.SnapshotRef != parent.SnapshotRef {
		return empty, errToolsUpstream
	}
	return result, nil
}
