package storepg

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	"tokendancechat/backend/store"
)

// --- Notification preferences ---

// SetNotificationPrefs upserts one preference.
func (s *Store) SetNotificationPrefs(username, key string, mutedUntil int64, showPreview bool) error {
	ctx, cancel := s.opCtx()
	defer cancel()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO notification_prefs (username, key, muted_until, show_preview)
		 VALUES ($1,$2,$3,$4)
		 ON CONFLICT (username, key) DO UPDATE SET
		   muted_until = excluded.muted_until,
		   show_preview = excluded.show_preview`,
		username, key, nullableTime(mutedUntil), showPreview)
	return err
}

// GetNotificationPrefs reads one preference. Missing rows default to
// muted=0, preview=true, matching the SQLite store.
func (s *Store) GetNotificationPrefs(username, key string) (mutedUntil int64, showPreview bool, err error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	var muted pgtype.Timestamptz
	err = s.pool.QueryRow(ctx,
		"SELECT muted_until, show_preview FROM notification_prefs WHERE username=$1 AND key=$2",
		username, key).Scan(&muted, &showPreview)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, true, nil
	}
	if err != nil {
		return 0, true, err
	}
	return unixMilliOrZero(muted), showPreview, nil
}

// ListNotificationPrefs lists a user's preferences. SQLite exposed rowid order;
// PostgreSQL orders by key because the order is not user-visible.
func (s *Store) ListNotificationPrefs(username string) []store.NotificationPref {
	ctx, cancel := s.opCtx()
	defer cancel()
	rows, err := s.pool.Query(ctx,
		"SELECT key, muted_until, show_preview FROM notification_prefs WHERE username=$1 ORDER BY key",
		username)
	if err != nil {
		return nil
	}
	defer rows.Close()
	prefs := []store.NotificationPref{}
	for rows.Next() {
		var (
			p     store.NotificationPref
			muted pgtype.Timestamptz
		)
		if err := rows.Scan(&p.Key, &muted, &p.ShowPreview); err != nil {
			continue
		}
		p.MutedUntil = unixMilliOrZero(muted)
		prefs = append(prefs, p)
	}
	if err := rows.Err(); err != nil {
		log.Printf("storepg: ListNotificationPrefs iteration error: %v", err)
	}
	return prefs
}

// --- Profiles ---

// UpsertUserProfile writes a profile snapshot.
func (s *Store) UpsertUserProfile(username, displayName, avatarURL, bio, status string, lastSeen int64) error {
	ctx, cancel := s.opCtx()
	defer cancel()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_profiles (username, display_name, avatar_url, bio, status, last_seen)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (username) DO UPDATE SET
		   display_name = excluded.display_name,
		   avatar_url = excluded.avatar_url,
		   bio = excluded.bio,
		   status = excluded.status,
		   last_seen = excluded.last_seen`,
		username, displayName, avatarURL, bio, status, nullableTime(lastSeen))
	return err
}

// GetUserProfile reads one profile.
func (s *Store) GetUserProfile(username string) (*store.UserProfile, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	var (
		p        store.UserProfile
		lastSeen pgtype.Timestamptz
	)
	err := s.pool.QueryRow(ctx,
		"SELECT username, display_name, avatar_url, bio, status, last_seen FROM user_profiles WHERE username=$1",
		username).Scan(&p.Username, &p.DisplayName, &p.AvatarURL, &p.Bio, &p.Status, &lastSeen)
	if err != nil {
		return nil, err
	}
	p.LastSeen = unixMilliOrZero(lastSeen)
	return &p, nil
}

// UpdateUserLastSeen refreshes the presence timestamp.
func (s *Store) UpdateUserLastSeen(username string) error {
	ctx, cancel := s.opCtx()
	defer cancel()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_profiles (username, last_seen) VALUES ($1,$2)
		 ON CONFLICT (username) DO UPDATE SET last_seen = excluded.last_seen`,
		username, time.Now())
	return err
}

// GetAllUserProfiles lists every profile ordered by username.
func (s *Store) GetAllUserProfiles() ([]store.UserProfile, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	rows, err := s.pool.Query(ctx,
		"SELECT username, display_name, avatar_url, bio, status, last_seen FROM user_profiles ORDER BY username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	profiles := []store.UserProfile{}
	for rows.Next() {
		var (
			p        store.UserProfile
			lastSeen pgtype.Timestamptz
		)
		if err := rows.Scan(&p.Username, &p.DisplayName, &p.AvatarURL, &p.Bio, &p.Status, &lastSeen); err != nil {
			continue
		}
		p.LastSeen = unixMilliOrZero(lastSeen)
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}

// --- Polls ---

// CreatePoll inserts a poll. IsClosed is always false on creation, matching
// the SQLite store.
func (s *Store) CreatePoll(poll *store.Poll) error {
	ctx, cancel := s.opCtx()
	defer cancel()
	optsJSON, _ := json.Marshal(poll.Options)
	votesJSON, _ := json.Marshal(poll.Votes)
	votersJSON, _ := json.Marshal(poll.Voters)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO polls (id, room_id, creator, question, options, multiple_choice, is_anonymous, is_closed, votes, voters, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,false,$8,$9,$10)`,
		poll.ID, poll.RoomID, poll.Creator, poll.Question, string(optsJSON),
		poll.MultipleChoice, poll.IsAnonymous, string(votesJSON), string(votersJSON), time.UnixMilli(poll.CreatedAt))
	return err
}

// GetPoll reads a poll.
func (s *Store) GetPoll(pollID string) (*store.Poll, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	var (
		p                          store.Poll
		optsJSON, votesJSON, voter []byte
		createdAt                  time.Time
	)
	err := s.pool.QueryRow(ctx,
		`SELECT id, room_id, creator, question, options, multiple_choice, is_anonymous, is_closed, votes, voters, created_at
		 FROM polls WHERE id=$1`, pollID).
		Scan(&p.ID, &p.RoomID, &p.Creator, &p.Question, &optsJSON, &p.MultipleChoice,
			&p.IsAnonymous, &p.IsClosed, &votesJSON, &voter, &createdAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(optsJSON, &p.Options)
	json.Unmarshal(votesJSON, &p.Votes)
	json.Unmarshal(voter, &p.Voters)
	p.CreatedAt = createdAt.UnixMilli()
	if p.Options == nil {
		p.Options = []string{}
	}
	if p.Votes == nil {
		p.Votes = make(map[int]int)
	}
	if p.Voters == nil {
		p.Voters = make(map[int][]string)
	}
	return &p, nil
}

// VotePoll records a vote once per user, serialized by a row lock.
func (s *Store) VotePoll(pollID string, username string, optionIndex int) error {
	ctx, cancel := s.opCtx()
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var (
		optsJSON, votesJSON, voter []byte
	)
	if err := tx.QueryRow(ctx,
		"SELECT options, votes, voters FROM polls WHERE id=$1 FOR UPDATE", pollID).
		Scan(&optsJSON, &votesJSON, &voter); err != nil {
		return err
	}
	var (
		options []string
		votes   map[int]int
		voters  map[int][]string
	)
	json.Unmarshal(optsJSON, &options)
	json.Unmarshal(votesJSON, &votes)
	json.Unmarshal(voter, &voters)
	if votes == nil {
		votes = make(map[int]int)
	}
	if voters == nil {
		voters = make(map[int][]string)
	}
	for _, v := range voters[optionIndex] {
		if v == username {
			return nil
		}
	}
	votes[optionIndex]++
	voters[optionIndex] = append(voters[optionIndex], username)
	newVotesJSON, _ := json.Marshal(votes)
	newVotersJSON, _ := json.Marshal(voters)
	if _, err := tx.Exec(ctx, "UPDATE polls SET votes=$1, voters=$2 WHERE id=$3",
		string(newVotesJSON), string(newVotersJSON), pollID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ClosePoll closes a poll.
func (s *Store) ClosePoll(pollID string) error {
	ctx, cancel := s.opCtx()
	defer cancel()
	_, err := s.pool.Exec(ctx, "UPDATE polls SET is_closed=true WHERE id=$1", pollID)
	return err
}

// --- Custom emojis ---

// AddCustomEmoji registers an uploaded emoji.
func (s *Store) AddCustomEmoji(name, url, uploader, roomID string) error {
	ctx, cancel := s.opCtx()
	defer cancel()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO custom_emojis (id, name, url, uploader, room_id, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6)`,
		uuid.New().String(), name, url, uploader, roomID, time.Now())
	return err
}

// ListCustomEmojis lists emojis visible in a room.
func (s *Store) ListCustomEmojis(roomID string) ([]store.CustomEmoji, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	var rows pgx.Rows
	var err error
	if roomID != "" {
		rows, err = s.pool.Query(ctx,
			`SELECT id, name, url, uploader, room_id, created_at FROM custom_emojis
			 WHERE room_id=$1 OR room_id='' ORDER BY created_at DESC`, roomID)
	} else {
		rows, err = s.pool.Query(ctx,
			"SELECT id, name, url, uploader, room_id, created_at FROM custom_emojis ORDER BY created_at DESC")
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []store.CustomEmoji
	for rows.Next() {
		var (
			e         store.CustomEmoji
			createdAt time.Time
		)
		if err := rows.Scan(&e.ID, &e.Name, &e.URL, &e.Uploader, &e.RoomID, &createdAt); err != nil {
			return nil, err
		}
		e.CreatedAt = createdAt.UnixMilli()
		result = append(result, e)
	}
	return result, rows.Err()
}

// DeleteCustomEmoji removes an emoji owned by the caller.
func (s *Store) DeleteCustomEmoji(name, username string) error {
	ctx, cancel := s.opCtx()
	defer cancel()
	_, err := s.pool.Exec(ctx,
		"DELETE FROM custom_emojis WHERE name=$1 AND uploader=$2", name, username)
	return err
}

// SearchCustomEmojis searches names by prefix. ILIKE replaces SQLite's
// ASCII-case-insensitive LIKE.
func (s *Store) SearchCustomEmojis(query string) ([]store.CustomEmoji, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, url, uploader, room_id, created_at FROM custom_emojis
		 WHERE name ILIKE $1 ESCAPE '\' ORDER BY created_at DESC LIMIT 20`,
		strings.ReplaceAll(query, "_", `\_`)+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []store.CustomEmoji
	for rows.Next() {
		var (
			e         store.CustomEmoji
			createdAt time.Time
		)
		if err := rows.Scan(&e.ID, &e.Name, &e.URL, &e.Uploader, &e.RoomID, &createdAt); err != nil {
			return nil, err
		}
		e.CreatedAt = createdAt.UnixMilli()
		result = append(result, e)
	}
	return result, rows.Err()
}

// --- Export ---

// ExportMessages returns a bounded export of one conversation.
func (s *Store) ExportMessages(ctx context.Context, roomID, toUser, _ string, username string, limit int) ([]store.StoredMessage, error) {
	if limit <= 0 {
		limit = 10000
	}
	var (
		rows pgx.Rows
		err  error
	)
	switch {
	case toUser != "" && username != "":
		rows, err = s.pool.Query(ctx, "SELECT "+messageColumns+
			` FROM messages WHERE deleted=false
			 AND ((username=$1 AND to_user=$2) OR (username=$3 AND to_user=$4))
			 ORDER BY created_at ASC, seq ASC LIMIT $5`,
			username, toUser, toUser, username, limit)
	case roomID != "":
		rows, err = s.pool.Query(ctx, "SELECT "+messageColumns+
			" FROM messages WHERE room_id=$1 AND deleted=false ORDER BY created_at ASC, seq ASC LIMIT $2",
			roomID, limit)
	default:
		rows, err = s.pool.Query(ctx, "SELECT "+messageColumns+
			" FROM messages WHERE to_user='' AND deleted=false ORDER BY created_at ASC, seq ASC LIMIT $1",
			limit)
	}
	if err != nil {
		return nil, fmt.Errorf("storepg: ExportMessages query error: %w", err)
	}
	defer rows.Close()
	messages := make([]store.StoredMessage, 0, limit)
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			log.Printf("storepg: ExportMessages scan error: %v", err)
			continue
		}
		if m.Deleted {
			m.Content = ""
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storepg: ExportMessages iteration error: %w", err)
	}
	s.enrichReactions(ctx, messages)
	return messages, nil
}

// --- Local auth ---

const (
	inviteCodeChars  = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	inviteCodeLength = 8
)

func hashPassword(password string) string {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		panic("bcrypt: " + err.Error())
	}
	return string(hash)
}

func checkPassword(hash, password string) (bool, string) {
	if strings.HasPrefix(hash, "$2a$") {
		err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
		return err == nil, ""
	}
	parts := strings.SplitN(hash, ":", 2)
	if len(parts) != 2 {
		return false, ""
	}
	salt, err := hex.DecodeString(parts[0])
	if err != nil {
		return false, ""
	}
	h := sha256.Sum256(append(salt, []byte(password)...))
	if hex.EncodeToString(h[:]) != parts[1] {
		return false, ""
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return true, ""
	}
	return true, string(newHash)
}

var (
	dummyHashOnce sync.Once
	dummyHash     []byte
)

func dummyPasswordHash() []byte {
	dummyHashOnce.Do(func() {
		dummyHash, _ = bcrypt.GenerateFromPassword([]byte("tokenbot-timing-equaliser"), 12)
	})
	return dummyHash
}

func randomCode(length int) string {
	result := make([]byte, length)
	for i := range result {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(inviteCodeChars))))
		if err != nil {
			n = big.NewInt(int64(time.Now().UnixNano() % int64(len(inviteCodeChars))))
		}
		result[i] = inviteCodeChars[n.Int64()]
	}
	return string(result)
}

// RegisterUser validates the invite code, hashes the password, creates the
// user and increments the invite use count in one transaction.
func (s *Store) RegisterUser(username, passwordHash, inviteCode string) error {
	code := strings.TrimSpace(strings.ToUpper(inviteCode))
	if code == "" {
		return fmt.Errorf("invite code is required")
	}
	ctx, cancel := s.opCtx()
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var creator string
	var maxUses, useCount int
	err = tx.QueryRow(ctx,
		"SELECT creator, max_uses, use_count FROM invite_codes WHERE code=$1 FOR UPDATE",
		code).Scan(&creator, &maxUses, &useCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("invalid invite code")
	}
	if err != nil {
		return err
	}
	if useCount >= maxUses {
		return fmt.Errorf("invite code has no remaining uses")
	}
	var existing int
	if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM users WHERE username=$1", username).Scan(&existing); err != nil {
		return err
	}
	if existing > 0 {
		return fmt.Errorf("username already registered")
	}
	hash := hashPassword(passwordHash)
	if _, err := tx.Exec(ctx,
		"INSERT INTO users (username, password_hash, invited_by, created_at) VALUES ($1,$2,$3,$4)",
		username, hash, creator, time.Now()); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE invite_codes SET use_count = use_count + 1 WHERE code=$1", code); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// VerifyUser checks a local password, including legacy SHA-256 upgrades.
func (s *Store) VerifyUser(username, password string) (bool, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	var passwordHash string
	err := s.pool.QueryRow(ctx, "SELECT password_hash FROM users WHERE username=$1", username).Scan(&passwordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash(), []byte(password))
		return false, nil
	}
	if err != nil {
		return false, err
	}
	ok, upgradeHash := checkPassword(passwordHash, password)
	if !ok {
		return false, nil
	}
	if upgradeHash != "" {
		_, _ = s.pool.Exec(ctx, "UPDATE users SET password_hash=$1 WHERE username=$2", upgradeHash, username)
	}
	return true, nil
}

// UserExists reports whether a local account exists.
func (s *Store) UserExists(username string) (bool, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	var count int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM users WHERE username=$1", username).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// GenerateInviteCode creates a unique random invite code.
func (s *Store) GenerateInviteCode(creator string, maxUses int) (string, error) {
	if maxUses <= 0 {
		maxUses = 5
	}
	ctx, cancel := s.opCtx()
	defer cancel()
	var code string
	for {
		code = randomCode(inviteCodeLength)
		var count int
		if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM invite_codes WHERE code=$1", code).Scan(&count); err != nil {
			return "", err
		}
		if count == 0 {
			break
		}
	}
	_, err := s.pool.Exec(ctx,
		"INSERT INTO invite_codes (code, creator, max_uses, use_count, created_at) VALUES ($1,$2,$3,0,$4)",
		code, creator, maxUses, time.Now())
	if err != nil {
		return "", err
	}
	return code, nil
}

// ListInviteCodes lists codes created by a user.
func (s *Store) ListInviteCodes(creator string) ([]store.InviteCodeRecord, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	rows, err := s.pool.Query(ctx,
		"SELECT code, creator, max_uses, use_count, created_at FROM invite_codes WHERE creator=$1 ORDER BY created_at DESC",
		creator)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	codes := []store.InviteCodeRecord{}
	for rows.Next() {
		var (
			c         store.InviteCodeRecord
			createdAt time.Time
		)
		if err := rows.Scan(&c.Code, &c.Creator, &c.MaxUses, &c.UseCount, &createdAt); err != nil {
			continue
		}
		c.CreatedAt = createdAt.UnixMilli()
		codes = append(codes, c)
	}
	return codes, rows.Err()
}

// ValidateInviteCode checks whether a code exists and has remaining uses.
func (s *Store) ValidateInviteCode(code string) (bool, error) {
	code = strings.TrimSpace(strings.ToUpper(code))
	if code == "" {
		return false, nil
	}
	ctx, cancel := s.opCtx()
	defer cancel()
	var maxUses, useCount int
	err := s.pool.QueryRow(ctx,
		"SELECT max_uses, use_count FROM invite_codes WHERE code=$1", code).Scan(&maxUses, &useCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return useCount < maxUses, nil
}

// --- OIDC ---

// OIDCLogoutGeneration returns the current subject logout generation.
func (s *Store) OIDCLogoutGeneration(sub string) (int64, error) {
	sub = strings.TrimSpace(sub)
	if sub == "" {
		return 0, fmt.Errorf("OIDC subject is required")
	}
	ctx, cancel := s.opCtx()
	defer cancel()
	var generation int64
	err := s.pool.QueryRow(ctx,
		"SELECT generation FROM oidc_logout_epochs WHERE sub=$1", sub).Scan(&generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return generation, nil
}

// ApplyOIDCBackchannelLogout records a Logout Token JTI and advances the
// subject generation atomically. An advisory lock serializes it against
// WithBotRunPublication for the same subject, preserving the SQLite RLock
// ordering with PostgreSQL primitives.
func (s *Store) ApplyOIDCBackchannelLogout(
	sub, jti, claimsSHA256 string,
	receivedAt, expiresAt time.Time,
) (store.OIDCBackchannelLogoutResult, error) {
	sub = strings.TrimSpace(sub)
	jti = strings.TrimSpace(jti)
	claimsSHA256 = strings.TrimSpace(claimsSHA256)
	if sub == "" || jti == "" || claimsSHA256 == "" {
		return store.OIDCBackchannelLogoutResult{}, fmt.Errorf("OIDC logout sub, jti, and claims hash are required")
	}
	if !expiresAt.After(receivedAt) {
		return store.OIDCBackchannelLogoutResult{}, fmt.Errorf("OIDC logout expiry must be after receipt")
	}
	ctx, cancel := s.opCtx()
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.OIDCBackchannelLogoutResult{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "oidc-logout:"+sub); err != nil {
		return store.OIDCBackchannelLogoutResult{}, err
	}
	if err := lockBudgetLedger(ctx, tx); err != nil {
		return store.OIDCBackchannelLogoutResult{}, err
	}

	result := store.OIDCBackchannelLogoutResult{}
	tag, err := tx.Exec(ctx,
		`INSERT INTO oidc_backchannel_logout_jtis (jti, claims_sha256, received_at, expires_at)
		 VALUES ($1,$2,$3,$4) ON CONFLICT (jti) DO NOTHING`,
		jti, claimsSHA256, receivedAt, expiresAt)
	if err != nil {
		return store.OIDCBackchannelLogoutResult{}, err
	}
	if tag.RowsAffected() == 0 {
		var existingHash string
		if err := tx.QueryRow(ctx,
			"SELECT claims_sha256 FROM oidc_backchannel_logout_jtis WHERE jti=$1", jti).Scan(&existingHash); err != nil {
			return store.OIDCBackchannelLogoutResult{}, err
		}
		if existingHash != claimsSHA256 {
			return store.OIDCBackchannelLogoutResult{}, store.ErrOIDCLogoutJTIReuse
		}
		result.Duplicate = true
	} else {
		if _, err := tx.Exec(ctx,
			"DELETE FROM oidc_backchannel_logout_jtis WHERE expires_at < $1", receivedAt); err != nil {
			return store.OIDCBackchannelLogoutResult{}, err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO oidc_logout_epochs (sub, generation, updated_at)
			 VALUES ($1, 1, $2)
			 ON CONFLICT (sub) DO UPDATE SET
			   generation = oidc_logout_epochs.generation + 1,
			   updated_at = excluded.updated_at`,
			sub, receivedAt); err != nil {
			return store.OIDCBackchannelLogoutResult{}, err
		}
	}
	var generation int64
	if err := tx.QueryRow(ctx,
		"SELECT generation FROM oidc_logout_epochs WHERE sub=$1", sub).Scan(&generation); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return store.OIDCBackchannelLogoutResult{}, err
	}
	result.Generation = generation
	err = tx.QueryRow(ctx, "SELECT chat_username FROM oidc_users WHERE sub=$1", sub).Scan(&result.ChatUsername)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return store.OIDCBackchannelLogoutResult{}, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE bot_runs SET charged_tokens=CASE WHEN status='queued' THEN 0 ELSE charged_tokens END,
		 status='cancelled', reason='account_logout', updated_at=$1
		 WHERE actor_id=$2 AND session_generation<$3 AND status IN ('queued','running')`,
		receivedAt, "oidc:"+sub, result.Generation); err != nil {
		return store.OIDCBackchannelLogoutResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.OIDCBackchannelLogoutResult{}, err
	}
	return result, nil
}

// UpsertOIDCUser inserts or updates an OIDC identity mapping.
func (s *Store) UpsertOIDCUser(profile store.OIDCUserProfile) (string, error) {
	if strings.TrimSpace(profile.Sub) == "" {
		return "", fmt.Errorf("OIDC subject is required")
	}
	ctx, cancel := s.opCtx()
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	// Serialize username allocation across instances.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "oidc-user-allocate"); err != nil {
		return "", err
	}

	var existingUsername string
	err = tx.QueryRow(ctx, "SELECT chat_username FROM oidc_users WHERE sub=$1", profile.Sub).Scan(&existingUsername)
	if err == nil {
		now := time.Now().UnixMilli()
		if _, err := tx.Exec(ctx,
			`UPDATE oidc_users SET email=$1, preferred_username=$2, name=$3, picture=$4, updated_at=$5 WHERE sub=$6`,
			profile.Email, profile.PreferredUsername, profile.Name, profile.Picture, time.UnixMilli(now), profile.Sub); err != nil {
			return "", err
		}
		if err := syncOIDCProfile(ctx, tx, existingUsername, profile); err != nil {
			return "", err
		}
		if err := tx.Commit(ctx); err != nil {
			return "", err
		}
		return existingUsername, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	base := strings.TrimSpace(profile.DesiredUsername)
	if base == "" {
		digest := sha256.Sum256([]byte(profile.Sub))
		base = "td_" + hex.EncodeToString(digest[:6])
	}
	digest := sha256.Sum256([]byte(profile.Sub))
	suffix := hex.EncodeToString(digest[:4])
	resolvedUsername := ""
	for attempt := 0; attempt < 32; attempt++ {
		candidateSuffix := ""
		if attempt > 0 {
			candidateSuffix = "_" + suffix
			if attempt > 1 {
				candidateSuffix += fmt.Sprintf("_%d", attempt)
			}
		}
		candidateBase := base
		maxBase := 20 - len(candidateSuffix)
		if maxBase < 2 {
			candidateBase = "td"
			maxBase = 20 - len(candidateSuffix)
		}
		if len(candidateBase) > maxBase {
			candidateBase = truncateUsernameRunes(candidateBase, maxBase)
		}
		candidateBase = strings.TrimRight(candidateBase, "_")
		if candidateBase == "" {
			candidateBase = "td"
		}
		candidate := candidateBase + candidateSuffix
		var exists int
		err := tx.QueryRow(ctx,
			`SELECT 1 FROM oidc_users WHERE chat_username=$1
			 UNION ALL SELECT 1 FROM users WHERE username=$2
			 UNION ALL SELECT 1 FROM guest_identities WHERE handle=$3 LIMIT 1`,
			candidate, candidate, candidate).Scan(&exists)
		if errors.Is(err, pgx.ErrNoRows) {
			resolvedUsername = candidate
			break
		}
		if err != nil {
			return "", err
		}
	}
	if resolvedUsername == "" {
		return "", fmt.Errorf("could not allocate a stable OIDC username")
	}
	now := time.Now()
	if _, err := tx.Exec(ctx,
		`INSERT INTO oidc_users (sub, chat_username, email, preferred_username, name, picture, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$7)`,
		profile.Sub, resolvedUsername, profile.Email, profile.PreferredUsername,
		profile.Name, profile.Picture, now); err != nil {
		return "", err
	}
	if err := syncOIDCProfile(ctx, tx, resolvedUsername, profile); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return resolvedUsername, nil
}

func syncOIDCProfile(ctx context.Context, tx pgx.Tx, username string, profile store.OIDCUserProfile) error {
	displayName := strings.TrimSpace(profile.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(profile.PreferredUsername)
	}
	if displayName == "" && profile.Email != "" {
		displayName, _, _ = strings.Cut(profile.Email, "@")
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO user_profiles (username, display_name, avatar_url, bio, status, last_seen)
		 VALUES ($1,$2,$3,'','',NULL)
		 ON CONFLICT (username) DO UPDATE SET
		   display_name = excluded.display_name,
		   avatar_url = excluded.avatar_url`,
		username, displayName, profile.Picture)
	return err
}

// GetOIDCUserBySub reads an identity by Account subject.
func (s *Store) GetOIDCUserBySub(sub string) (*store.OIDCUser, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	return scanOIDCUser(s.pool.QueryRow(ctx,
		"SELECT sub, chat_username, email, preferred_username, name, picture, created_at, updated_at FROM oidc_users WHERE sub=$1", sub))
}

// GetOIDCUserByUsername reads an identity by Chat username.
func (s *Store) GetOIDCUserByUsername(username string) (*store.OIDCUser, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	return scanOIDCUser(s.pool.QueryRow(ctx,
		"SELECT sub, chat_username, email, preferred_username, name, picture, created_at, updated_at FROM oidc_users WHERE chat_username=$1", username))
}

func scanOIDCUser(row pgx.Row) (*store.OIDCUser, error) {
	var (
		u                  store.OIDCUser
		createdAt, updated time.Time
	)
	if err := row.Scan(&u.Sub, &u.ChatUsername, &u.Email, &u.PreferredUsername,
		&u.Name, &u.Picture, &createdAt, &updated); err != nil {
		return nil, err
	}
	u.CreatedAt = createdAt.UnixMilli()
	u.UpdatedAt = updated.UnixMilli()
	return &u, nil
}

// truncateUsernameRunes cuts a candidate handle by characters, never bytes, so
// CJK and emoji names stay valid UTF-8 for PostgreSQL.
func truncateUsernameRunes(value string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
