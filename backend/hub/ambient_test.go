package hub

import (
	"testing"
	"time"

	"tokendancechat/backend/store"
)

func TestParseAmbientDecision(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		action string
	}{
		{"plain reply", `{"action":"reply","reason":"direct question"}`, "reply"},
		{"fenced", "```json\n{\"action\":\"reply\",\"reason\":\"needs help\"}\n```", "reply"},
		{"prose around", `Decision: {"action":"ignore","reason":"small talk"} done`, "ignore"},
		{"unknown action", `{"action":"maybe"}`, "ignore"},
		{"unparseable", `not json`, "ignore"},
		{"plain reply word", `I think TokenBot should reply here.`, "reply"},
		{"plain ignore word", `This is small talk, ignore it.`, "ignore"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			action, reason := parseAmbientDecision(tc.raw)
			if action != tc.action {
				t.Fatalf("action = %q, want %q", action, tc.action)
			}
			if reason == "" {
				t.Fatal("reason must never be empty")
			}
		})
	}
}

func TestReserveAmbientSlotHonorsCooldownAndPacing(t *testing.T) {
	h := &Hub{
		ambient:         ambientPolicy{enabled: true, cooldown: time.Minute, perHour: 2},
		runs:            map[string]*botRun{},
		runQueues:       map[string]*conversationQueue{},
		runStore:        nil,
	}
	if !h.reserveAmbientSlot("room") {
		t.Fatal("first slot should be reserved")
	}
	h.ambientMu.Lock()
	h.ambientInFlight = false
	h.ambientMu.Unlock()
	if h.reserveAmbientSlot("room") {
		t.Fatal("cooldown must block a second slot")
	}

	h.ambientMu.Lock()
	h.ambientLast = time.Now().Add(-2 * time.Minute)
	h.ambientTimes = []time.Time{time.Now(), time.Now()}
	h.ambientMu.Unlock()
	if h.reserveAmbientSlot("room") {
		t.Fatal("hourly pacing must block once perHour is reached")
	}
}

func TestReserveAmbientSlotYieldsToActiveRun(t *testing.T) {
	h := &Hub{
		ambient:   ambientPolicy{enabled: true, cooldown: 0, perHour: 10},
		runs:      map[string]*botRun{"active": {mode: "public", record: store.BotRun{ID: "active", RoomID: "room", Status: "running"}}},
		runQueues: map[string]*conversationQueue{},
	}
	if h.reserveAmbientSlot("room") {
		t.Fatal("an active public run must own the room")
	}
}

func TestMaybeAmbientReplyDisabledDoesNothing(t *testing.T) {
	h := &Hub{ambient: ambientPolicy{enabled: false}}
	trigger := StoredMessage{Username: "alice", Content: "a real question", RoomID: "room"}
	h.maybeAmbientReply(nil, trigger)
	h.ambientMu.Lock()
	defer h.ambientMu.Unlock()
	if h.ambientInFlight {
		t.Fatal("disabled ambient mode must not create work")
	}
}
