package linking

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalid         = errors.New("invalid account-link input")
	ErrConflict        = errors.New("account-link conflict")
	ErrNotFound        = errors.New("account-link not found")
	ErrUnauthenticated = errors.New("DataCenter session unauthenticated")
	ErrUnavailable     = errors.New("account-link dependency unavailable")
)

type Link struct {
	UID      int64  `json:"uid,string"`
	DCUserID string `json:"dc_user_id"`
	Revision int64  `json:"revision"`
	State    string `json:"state"`
}

type Store struct{ db *pgxpool.Pool }

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

func conflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "23505" || pgErr.Code == "23503") {
		return fmt.Errorf("%w: unique account identity", ErrConflict)
	}
	return err
}

// Bind reserves a DC UUID permanently for this RTW UID. An active UID must
// first disable its old link before binding a different DC account. No email,
// display name, or client-supplied UID participates in the transaction.
func (s *Store) Bind(ctx context.Context, uid int64, dcUserID string, expectedRevision int64) (Link, error) {
	if s == nil || s.db == nil || uid <= 0 || dcUserID == "" || expectedRevision < 0 {
		return Link{}, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Link{}, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO rtw_dc_link_identities(dc_user_id,uid)
		VALUES($1,$2) ON CONFLICT DO NOTHING`, dcUserID, uid)
	if err != nil {
		return Link{}, conflict(err)
	}
	var reservedUID int64
	if err := tx.QueryRow(ctx, `SELECT uid FROM rtw_dc_link_identities
		WHERE dc_user_id=$1 FOR UPDATE`, dcUserID).Scan(&reservedUID); err != nil {
		return Link{}, err
	}
	if reservedUID != uid {
		return Link{}, fmt.Errorf("%w: DC account reserved for another UID", ErrConflict)
	}
	var old Link
	err = tx.QueryRow(ctx, `SELECT dc_user_id,revision,state FROM rtw_dc_account_links
		WHERE uid=$1 FOR UPDATE`, uid).Scan(&old.DCUserID, &old.Revision, &old.State)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if expectedRevision != 0 {
			return Link{}, fmt.Errorf("%w: first link requires revision zero", ErrConflict)
		}
		_, err = tx.Exec(ctx, `INSERT INTO rtw_dc_account_links(uid,dc_user_id,revision,state)
			VALUES($1,$2,1,'active')`, uid, dcUserID)
		if err != nil {
			return Link{}, conflict(err)
		}
		old = Link{UID: uid, DCUserID: dcUserID, Revision: 1, State: "active"}
	case err != nil:
		return Link{}, err
	case old.State == "active" && old.DCUserID == dcUserID:
		old.UID = uid // same verified pair is an idempotent replay
	case old.State == "active":
		return Link{}, fmt.Errorf("%w: disable current DC account before rebind", ErrConflict)
	case old.State == "disabled":
		if expectedRevision != old.Revision {
			return Link{}, fmt.Errorf("%w: stale link revision", ErrConflict)
		}
		_, err = tx.Exec(ctx, `UPDATE rtw_dc_account_links
			SET dc_user_id=$2,revision=revision+1,state='active',updated_at=now()
			WHERE uid=$1`, uid, dcUserID)
		if err != nil {
			return Link{}, conflict(err)
		}
		old = Link{UID: uid, DCUserID: dcUserID, Revision: old.Revision + 1, State: "active"}
	default:
		return Link{}, ErrConflict
	}
	return old, tx.Commit(ctx)
}

func (s *Store) Disable(ctx context.Context, uid, expectedRevision int64) (Link, error) {
	if s == nil || s.db == nil || uid <= 0 || expectedRevision <= 0 {
		return Link{}, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Link{}, err
	}
	defer tx.Rollback(ctx)
	var link Link
	err = tx.QueryRow(ctx, `SELECT dc_user_id,revision,state FROM rtw_dc_account_links
		WHERE uid=$1 FOR UPDATE`, uid).Scan(&link.DCUserID, &link.Revision, &link.State)
	if errors.Is(err, pgx.ErrNoRows) {
		return Link{}, ErrNotFound
	}
	if err != nil {
		return Link{}, err
	}
	if link.State != "active" || link.Revision != expectedRevision {
		return Link{}, fmt.Errorf("%w: stale or inactive link", ErrConflict)
	}
	_, err = tx.Exec(ctx, `UPDATE rtw_dc_account_links
		SET state='disabled',revision=revision+1,updated_at=now() WHERE uid=$1`, uid)
	if err != nil {
		return Link{}, err
	}
	link.UID, link.Revision, link.State = uid, link.Revision+1, "disabled"
	return link, tx.Commit(ctx)
}

func (s *Store) ByUID(ctx context.Context, uid int64) (Link, error) {
	if s == nil || s.db == nil || uid <= 0 {
		return Link{}, ErrInvalid
	}
	var link Link
	err := s.db.QueryRow(ctx, `SELECT dc_user_id,revision,state FROM rtw_dc_account_links
		WHERE uid=$1`, uid).Scan(&link.DCUserID, &link.Revision, &link.State)
	if errors.Is(err, pgx.ErrNoRows) {
		return Link{}, ErrNotFound
	}
	link.UID = uid
	return link, err
}

func (s *Store) ActiveByDC(ctx context.Context, dcUserID string) (Link, error) {
	if s == nil || s.db == nil || dcUserID == "" {
		return Link{}, ErrInvalid
	}
	var link Link
	err := s.db.QueryRow(ctx, `SELECT l.uid,l.revision,l.state
		FROM rtw_dc_account_links l JOIN rtw_dc_link_identities i
			ON i.dc_user_id=l.dc_user_id AND i.uid=l.uid
		WHERE l.dc_user_id=$1 AND l.state='active'`, dcUserID).
		Scan(&link.UID, &link.Revision, &link.State)
	if errors.Is(err, pgx.ErrNoRows) {
		return Link{}, ErrNotFound
	}
	link.DCUserID = dcUserID
	return link, err
}
