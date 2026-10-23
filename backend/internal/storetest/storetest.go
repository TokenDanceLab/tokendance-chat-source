// Package storetest is a shared, implementation-agnostic contract suite for
// the chat persistence layer. It runs unchanged against the incumbent SQLite
// store and the PostgreSQL adapter, so behavioural drift fails one suite
// instead of hiding behind separate tests.
//
// The package is internal and has no production callers; it imports testing by
// design so the two runtimes execute exactly the same assertions.
package storetest

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"tokendancechat/backend/hub"
	"tokendancechat/backend/store"
)

// Store is the full persistence surface the application consumes: hub.Store,
// the handler's OIDC helpers, and the run/web extension interfaces.
type Store interface {
	hub.Store
	InsertActorMessage(actorID, username, content, replyToID, roomID, toUser, threadID string) (hub.StoredMessage, error)

	OIDCLogoutGeneration(sub string) (int64, error)
	ApplyOIDCBackchannelLogout(sub, jti, claimsSHA256 string, receivedAt, expiresAt time.Time) (store.OIDCBackchannelLogoutResult, error)

	WithBotRunPublication(string, func()) error
	SnapshotBotHistory(triggerID, roomID, user, bot, threadID string, private bool, limit int) ([]hub.StoredMessage, error)
	CreateBotRun(store.BotRun, store.RunBudgetLimits) (string, error)
	GetBotRun(string) (store.BotRun, error)
	StartBotRun(string) error
	FinishBotRun(string, string, string, int64, bool) error
	CompleteBotRun(string, string, json.RawMessage, int64, bool) (hub.StoredMessage, bool, error)
	CompleteBotRunSplit(string, []string, json.RawMessage, int64, bool) ([]hub.StoredMessage, bool, error)
	InterruptBotRuns() error
	RecentBotRuns(string, string, int) ([]store.BotRun, error)

	ReserveWebCall(string, string, string, string, int64) error
	SettleWebCall(string, string, bool) error
	SaveWebSource(store.WebSource) (store.WebSource, error)
	GetWebSource(string, string) (store.WebSource, error)
}

// Run executes every contract case against a fresh store per case.
func Run(t *testing.T, newStore func(t *testing.T) Store) {
	t.Helper()
	cases := []struct {
		name string
		fn   func(*testing.T, Store)
	}{
		{"Messages", testMessages},
		{"PrivateLanes", testPrivateLanes},
		{"Rooms", testRooms},
		{"Reactions", testReactions},
		{"Search", testSearch},
		{"PinsBlocksPrefs", testPinsBlocksPrefs},
		{"Threads", testThreads},
		{"Profiles", testProfiles},
		{"Polls", testPolls},
		{"CustomEmojis", testCustomEmojis},
		{"Export", testExport},
		{"LocalAuth", testLocalAuth},
		{"OIDCIdentity", testOIDCIdentity},
		{"RunLifecycle", testRunLifecycle},
		{"RunBudgetConcurrency", testRunBudgetConcurrency},
		{"RunBurstAndWeb", testRunBurstAndWeb},
		{"PublicationFence", testPublicationFence},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tc.fn(t, newStore(t))
		})
	}
}

// --- helpers shared by the cases ---

var hkt = time.FixedZone("HKT", 8*60*60)

func todayHKT() string {
	return time.Now().In(hkt).Format("2006-01-02")
}

func mustInsert(t *testing.T, m hub.StoredMessage, err error) hub.StoredMessage {
	t.Helper()
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	return m
}

func insertActor(t *testing.T, s Store, actorID, username, content, replyToID, roomID, toUser, threadID string) hub.StoredMessage {
	t.Helper()
	m, err := s.InsertActorMessage(actorID, username, content, replyToID, roomID, toUser, threadID)
	return mustInsert(t, m, err)
}

func insertParts(t *testing.T, s Store, username, content, roomID, toUser, threadID string, parts json.RawMessage) hub.StoredMessage {
	t.Helper()
	m, err := s.InsertMessageWithParts(username, content, roomID, toUser, threadID, parts)
	return mustInsert(t, m, err)
}

func publicMessage(t *testing.T, s Store, username, roomID, content string) hub.StoredMessage {
	t.Helper()
	return insertActor(t, s, "local:"+username, username, content, "", roomID, "", "")
}

func laneMessage(t *testing.T, s Store, username, bot, lane, content string) hub.StoredMessage {
	t.Helper()
	return insertActor(t, s, "local:"+username, username, content, "", "", bot, lane)
}

func newRun(id, messageID, triggerID, actorID, roomID, toUser, lane string) store.BotRun {
	return store.BotRun{
		ID:                id,
		MessageID:         messageID,
		TriggerMessageID:  triggerID,
		ActorID:           actorID,
		RequestedBy:       "alice",
		RoomID:            roomID,
		ToUser:            toUser,
		BotName:           "TokenBot",
		ThreadID:          lane,
		SessionGeneration: 0,
		SessionExpiresAt:  0,
		Input:             json.RawMessage(`{"trigger":"hi"}`),
		BudgetDay:         todayHKT(),
		IPKey:             "ip:10.0.0.1",
		ReservedTokens:    100,
		CreatedAt:         time.Now().UnixMilli(),
	}
}

func mustCreateRun(t *testing.T, s Store, r store.BotRun, limits store.RunBudgetLimits) {
	t.Helper()
	reason, err := s.CreateBotRun(r, limits)
	if err != nil {
		t.Fatalf("CreateBotRun(%s): %v", r.ID, err)
	}
	if reason != "" {
		t.Fatalf("CreateBotRun(%s) rejected: %q", r.ID, reason)
	}
}

func mustStartRun(t *testing.T, s Store, id string) {
	t.Helper()
	if err := s.StartBotRun(id); err != nil {
		t.Fatalf("StartBotRun(%s): %v", id, err)
	}
}

func findMessage(messages []hub.StoredMessage, id string) (hub.StoredMessage, bool) {
	for _, m := range messages {
		if m.ID == id {
			return m, true
		}
	}
	return hub.StoredMessage{}, false
}

func unique(prefix string) string {
	// time.Now() alone collides under Windows' coarse clock when goroutines
	// build fixtures in a tight loop.
	return prefix + "-" + uuid.NewString()
}
