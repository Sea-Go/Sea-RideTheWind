package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"sea-try-go/service/user/user/api/internal/svc"
	"sea-try-go/service/user/user/identity"
	"sea-try-go/service/user/user/identity/linking"

	"github.com/golang-jwt/jwt/v4"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/token"
)

// RegisterAccountLinkHandlers is an opt-in H01 candidate extension alongside
// generated legacy User Center routes. It is never registered by default.
func RegisterAccountLinkHandlers(server *rest.Server, service *svc.ServiceContext) {
	if service == nil || service.AccountLink == nil {
		return
	}
	rtwJWT := rtwAccountSession(service)
	server.AddRoutes(
		rest.WithMiddlewares([]rest.Middleware{rtwJWT}, []rest.Route{
			{Method: http.MethodGet, Path: "/account-link", Handler: currentAccountLink(service)},
			{Method: http.MethodPost, Path: "/account-link", Handler: bindAccountLink(service)},
			{Method: http.MethodDelete, Path: "/account-link", Handler: disableAccountLink(service)},
		}...),
		rest.WithPrefix("/usercenter/v1"),
	)
	server.AddRoutes([]rest.Route{{
		Method: http.MethodPost, Path: "/product-sessions/exchange", Handler: exchangeProductSession(service),
	}}, rest.WithPrefix("/usercenter/v1"))
}

// The two credentials must never reach go-zero's generic JWT failure logger
// together: that logger dumps full requests. Parse the RTW token with go-zero's
// public parser on a credential-only request, then pass the verified claim to
// the original request. The DC bearer stays only in Authorization.
func rtwAccountSession(service *svc.ServiceContext) rest.Middleware {
	parser := token.NewTokenParser()
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			rtwHeader := r.Header.Get("X-RTW-Authorization")
			if _, err := bearer(rtwHeader); err != nil {
				writeLinkJSON(w, http.StatusUnauthorized, map[string]string{"error": "rtw_session_invalid"})
				return
			}
			safe := r.Clone(r.Context())
			safe.Header = make(http.Header)
			safe.Header.Set("Authorization", rtwHeader)
			safe.Body = http.NoBody
			check := service.CheckBlacklistMiddleware
			if check == nil {
				writeLinkError(w, linking.ErrUnavailable)
				return
			}
			check(func(w http.ResponseWriter, checked *http.Request) {
				tok, err := parser.ParseToken(checked, service.Config.UserAuth.AccessSecret, "")
				if err != nil || tok == nil || !tok.Valid {
					writeLinkJSON(w, http.StatusUnauthorized, map[string]string{"error": "rtw_session_invalid"})
					return
				}
				claims, ok := tok.Claims.(jwt.MapClaims)
				if !ok {
					writeLinkJSON(w, http.StatusUnauthorized, map[string]string{"error": "rtw_session_invalid"})
					return
				}
				claim, ok := claims["userId"].(json.Number)
				if !ok {
					writeLinkJSON(w, http.StatusUnauthorized, map[string]string{"error": "rtw_session_invalid"})
					return
				}
				ctx := context.WithValue(r.Context(), "userId", claim)
				if _, err := identity.ClaimedUID(ctx); err != nil {
					writeLinkJSON(w, http.StatusUnauthorized, map[string]string{"error": "rtw_session_invalid"})
					return
				}
				next(w, r.WithContext(ctx))
			})(w, safe)
		}
	}
}

func bearer(header string) (string, error) {
	if !strings.HasPrefix(header, "Bearer ") {
		return "", linking.ErrInvalid
	}
	token := strings.TrimPrefix(header, "Bearer ")
	if token == "" || strings.TrimSpace(token) != token || strings.ContainsAny(token, " \t\r\n") {
		return "", linking.ErrInvalid
	}
	return token, nil
}

func revisionBody(w http.ResponseWriter, r *http.Request) (int64, bool) {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128))
	dec.DisallowUnknownFields()
	var body struct {
		ExpectedRevision *int64 `json:"expected_revision"`
	}
	if err := dec.Decode(&body); err != nil || body.ExpectedRevision == nil {
		writeLinkError(w, linking.ErrInvalid)
		return 0, false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeLinkError(w, linking.ErrInvalid)
		return 0, false
	}
	return *body.ExpectedRevision, true
}

func writeLinkJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeLinkError(w http.ResponseWriter, err error) {
	status, code := http.StatusServiceUnavailable, "dependency_unavailable"
	switch {
	case errors.Is(err, linking.ErrInvalid), errors.Is(err, identity.ErrInvalidClaim):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, linking.ErrUnauthenticated):
		status, code = http.StatusUnauthorized, "dc_session_invalid"
	case errors.Is(err, identity.ErrUserInactive), errors.Is(err, identity.ErrUserNotFound):
		status, code = http.StatusForbidden, "rtw_user_inactive"
	case errors.Is(err, linking.ErrNotFound):
		status, code = http.StatusNotFound, "account_link_missing"
	case errors.Is(err, linking.ErrConflict), errors.Is(err, identity.ErrIdentityMismatch):
		status, code = http.StatusConflict, "account_link_conflict"
	}
	writeLinkJSON(w, status, map[string]string{"error": code})
}

func currentAccountLink(service *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		link, err := service.AccountLink.Current(r.Context())
		if err != nil {
			writeLinkError(w, err)
			return
		}
		writeLinkJSON(w, http.StatusOK, link)
	}
}

func bindAccountLink(service *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		expected, ok := revisionBody(w, r)
		if !ok {
			return
		}
		dcBearer, err := bearer(r.Header.Get("Authorization"))
		if err != nil {
			writeLinkJSON(w, http.StatusUnauthorized, map[string]string{"error": "dc_session_invalid"})
			return
		}
		link, err := service.AccountLink.Bind(r.Context(), dcBearer, expected)
		if err != nil {
			writeLinkError(w, err)
			return
		}
		writeLinkJSON(w, http.StatusOK, link)
	}
}

func disableAccountLink(service *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		expected, ok := revisionBody(w, r)
		if !ok {
			return
		}
		link, err := service.AccountLink.Disable(r.Context(), expected)
		if err != nil {
			writeLinkError(w, err)
			return
		}
		writeLinkJSON(w, http.StatusOK, link)
	}
}

func exchangeProductSession(service *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dcBearer, err := bearer(r.Header.Get("Authorization"))
		if err != nil {
			writeLinkJSON(w, http.StatusUnauthorized, map[string]string{"error": "dc_session_invalid"})
			return
		}
		session, err := service.AccountLink.Exchange(r.Context(), dcBearer)
		if err != nil {
			writeLinkError(w, err)
			return
		}
		writeLinkJSON(w, http.StatusOK, session)
	}
}
