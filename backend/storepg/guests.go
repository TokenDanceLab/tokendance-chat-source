package storepg

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"tokendancechat/backend/store"
)

// ResolveGuestIdentity binds a guest cookie to the handle it first joins with
// and verifies that binding afterwards. Concurrent first joins are decided by
// the primary key / unique constraint, so two tabs can never end up bound to
// different handles.
func (s *Store) ResolveGuestIdentity(guestID, handle string) (string, error) {
	guestID = strings.TrimSpace(guestID)
	handle = strings.TrimSpace(handle)
	if guestID == "" {
		return "", store.ErrGuestIDRequired
	}
	if handle == "" {
		return "", store.ErrGuestHandleMismatch
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var bound string
	err := s.pool.QueryRow(ctx, "SELECT handle FROM guest_identities WHERE guest_id = $1", guestID).Scan(&bound)
	switch {
	case err == nil:
		if bound != handle {
			return bound, store.ErrGuestHandleMismatch
		}
		if _, err := s.pool.Exec(ctx, "UPDATE guest_identities SET last_seen_at = now() WHERE guest_id = $1", guestID); err != nil {
			return bound, err
		}
		return bound, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return "", err
	}

	var hasHistory bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM messages WHERE username = $1 AND room_id = '' AND to_user <> '')
		     OR EXISTS(SELECT 1 FROM messages WHERE to_user = $1 AND room_id = '')`,
		handle,
	).Scan(&hasHistory); err != nil {
		return "", err
	}
	if hasHistory {
		return "", store.ErrGuestHandleTaken
	}
	_, err = s.pool.Exec(ctx,
		"INSERT INTO guest_identities (guest_id, handle) VALUES ($1, $2) ON CONFLICT DO NOTHING",
		guestID, handle,
	)
	if err != nil {
		return "", err
	}
	// Whether the insert won or lost, the row is now authoritative.
	if err := s.pool.QueryRow(ctx, "SELECT handle FROM guest_identities WHERE guest_id = $1", guestID).Scan(&bound); err == nil {
		if bound != handle {
			return bound, store.ErrGuestHandleMismatch
		}
		return bound, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	return "", store.ErrGuestHandleTaken
}

// GuestHandle returns the handle bound to a guest cookie, if any.
func (s *Store) GuestHandle(guestID string) (string, bool) {
	if strings.TrimSpace(guestID) == "" {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var handle string
	if err := s.pool.QueryRow(ctx, "SELECT handle FROM guest_identities WHERE guest_id = $1", guestID).Scan(&handle); err != nil {
		return "", false
	}
	return handle, true
}
