package linking

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DCVerifier calls DataCenter's existing /v1/auth/me endpoint. That endpoint
// checks the current opaque access session, user state, and auth epoch.
type DCVerifier struct {
	endpoint string
	client   *http.Client
}

func NewDCVerifier(rawURL string, client *http.Client) (*DCVerifier, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		u.Path != "/v1/auth/me" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("%w: DataCenter auth/me endpoint", ErrInvalid)
	}
	// Plain HTTP is only an isolated loopback integration target.
	if u.Scheme == "http" && net.ParseIP(u.Hostname()) == nil && u.Hostname() != "localhost" {
		return nil, fmt.Errorf("%w: DataCenter non-loopback HTTP", ErrInvalid)
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && !net.ParseIP(u.Hostname()).IsLoopback() {
		return nil, fmt.Errorf("%w: DataCenter non-loopback HTTP", ErrInvalid)
	}
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	c := *client
	if c.Timeout == 0 || c.Timeout > 5*time.Second {
		c.Timeout = 5 * time.Second
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &DCVerifier{endpoint: u.String(), client: &c}, nil
}

// CurrentUserID returns the canonical DC UUID only when the bearer is valid
// now. A revoked or rotated access token gets an unauthorized response from DC.
func (v *DCVerifier) CurrentUserID(ctx context.Context, bearer string) (string, error) {
	if v == nil || v.client == nil || !strings.HasPrefix(bearer, "wh_access_") ||
		len(bearer) > 512 || strings.ContainsAny(bearer, " \t\r\n") {
		return "", ErrUnauthenticated
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")
	resp, err := v.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: DataCenter auth/me request: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", ErrUnauthenticated
	case http.StatusOK:
	default:
		return "", fmt.Errorf("%w: DataCenter auth/me status %d", ErrUnavailable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if err != nil || len(body) > 4096 {
		return "", fmt.Errorf("%w: DataCenter auth/me response", ErrUnavailable)
	}
	var user struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &user); err != nil {
		return "", fmt.Errorf("%w: DataCenter auth/me JSON", ErrUnavailable)
	}
	id, err := uuid.Parse(user.ID)
	if err != nil || id.String() != user.ID {
		return "", fmt.Errorf("%w: DataCenter auth/me UUID", ErrUnavailable)
	}
	return id.String(), nil
}
