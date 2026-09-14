package linking

import (
	"context"
	"errors"
	"fmt"
	"time"

	rtwjwt "sea-try-go/service/user/common/jwt"
	"sea-try-go/service/user/user/identity"

	"github.com/jackc/pgx/v5"
)

const ProductTTLSeconds int64 = 120

type Service struct {
	Store     *Store
	DC        *DCVerifier
	Users     identity.UserReader
	JWTSecret string
}

type ProductSession struct {
	Token         string `json:"token"`
	ExpiresAtUnix int64  `json:"expires_at_unix"`
	LinkRevision  int64  `json:"link_revision"`
}

func (s *Service) ready() bool {
	return s != nil && s.Store != nil && s.Store.db != nil && s.DC != nil &&
		s.Users != nil && s.JWTSecret != ""
}

// Bind requires go-zero's verified RTW JWT context and a currently valid DC
// bearer. Neither email equality nor a client-supplied UID can establish it.
func (s *Service) Bind(ctx context.Context, dcBearer string, expectedRevision int64) (Link, error) {
	if !s.ready() {
		return Link{}, ErrUnavailable
	}
	user, err := identity.ResolveUser(ctx, s.Users)
	if err != nil {
		return Link{}, err
	}
	dcID, err := s.DC.CurrentUserID(ctx, dcBearer)
	if err != nil {
		return Link{}, err
	}
	return s.Store.Bind(ctx, user.Uid, dcID, expectedRevision)
}

func (s *Service) Current(ctx context.Context) (Link, error) {
	if !s.ready() {
		return Link{}, ErrUnavailable
	}
	user, err := identity.ResolveUser(ctx, s.Users)
	if err != nil {
		return Link{}, err
	}
	return s.Store.ByUID(ctx, user.Uid)
}

// Disable requires the current RTW account session and a pointer revision.
// It does not transfer the historical DC UUID reservation to another UID.
func (s *Service) Disable(ctx context.Context, expectedRevision int64) (Link, error) {
	if !s.ready() {
		return Link{}, ErrUnavailable
	}
	user, err := identity.ResolveUser(ctx, s.Users)
	if err != nil {
		return Link{}, err
	}
	return s.Store.Disable(ctx, user.Uid, expectedRevision)
}

// Exchange verifies the DC bearer on every call, then holds RTW's active
// binding stable through User RPC validation and the short JWT signature.
// An already issued JWT is still accepted by existing knowledge routes until
// expiry; immediate DC revocation requires a separate product gate contract.
func (s *Service) Exchange(ctx context.Context, dcBearer string) (ProductSession, error) {
	if !s.ready() {
		return ProductSession{}, ErrUnavailable
	}
	dcID, err := s.DC.CurrentUserID(ctx, dcBearer)
	if err != nil {
		return ProductSession{}, err
	}
	tx, err := s.Store.db.Begin(ctx)
	if err != nil {
		return ProductSession{}, err
	}
	defer tx.Rollback(ctx)
	var uid, revision int64
	err = tx.QueryRow(ctx, `SELECT l.uid,l.revision FROM rtw_dc_account_links l
		JOIN rtw_dc_link_identities i ON i.dc_user_id=l.dc_user_id AND i.uid=l.uid
		WHERE l.dc_user_id=$1 AND l.state='active' FOR SHARE OF l`, dcID).Scan(&uid, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProductSession{}, ErrNotFound
	}
	if err != nil {
		return ProductSession{}, err
	}
	if _, err := identity.ActiveUID(ctx, s.Users, uid); err != nil {
		return ProductSession{}, err
	}
	now := time.Now().Unix()
	token, err := rtwjwt.GetToken(s.JWTSecret, now, ProductTTLSeconds, uid)
	if err != nil {
		return ProductSession{}, fmt.Errorf("%w: RTW product token", ErrUnavailable)
	}
	if err := tx.Commit(ctx); err != nil {
		return ProductSession{}, err
	}
	return ProductSession{Token: token, ExpiresAtUnix: now + ProductTTLSeconds,
		LinkRevision: revision}, nil
}
