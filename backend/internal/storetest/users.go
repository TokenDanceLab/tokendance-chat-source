package storetest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"tokendancechat/backend/store"
)

func testProfiles(t *testing.T, s Store) {
	if _, err := s.GetUserProfile("ghost"); err == nil {
		t.Fatal("missing profile must return an error")
	}
	if err := s.UpsertUserProfile("alice", "Alice", "https://example.com/a.png", "bio", "online", 1000); err != nil {
		t.Fatalf("UpsertUserProfile: %v", err)
	}
	p, err := s.GetUserProfile("alice")
	if err != nil {
		t.Fatalf("GetUserProfile: %v", err)
	}
	if p.DisplayName != "Alice" || p.AvatarURL != "https://example.com/a.png" || p.Bio != "bio" || p.Status != "online" || p.LastSeen != 1000 {
		t.Fatalf("profile = %+v", p)
	}
	if err := s.UpdateUserLastSeen("alice"); err != nil {
		t.Fatalf("UpdateUserLastSeen: %v", err)
	}
	p, _ = s.GetUserProfile("alice")
	if p.LastSeen <= 1000 {
		t.Fatalf("last_seen not advanced: %d", p.LastSeen)
	}
	if err := s.UpsertUserProfile("alice", "Alice 2", "", "", "", 0); err != nil {
		t.Fatalf("UpsertUserProfile update: %v", err)
	}
	all, err := s.GetAllUserProfiles()
	if err != nil {
		t.Fatalf("GetAllUserProfiles: %v", err)
	}
	if len(all) != 1 || all[0].DisplayName != "Alice 2" || all[0].LastSeen != 0 {
		t.Fatalf("profiles = %+v", all)
	}
}

func testPolls(t *testing.T, s Store) {
	poll := &store.Poll{
		ID:        unique("poll"),
		RoomID:    "room-poll",
		Creator:   "alice",
		Question:  "which?",
		Options:   []string{"a", "b"},
		Votes:     map[int]int{},
		Voters:    map[int][]string{},
		CreatedAt: time.Now().UnixMilli(),
	}
	if err := s.CreatePoll(poll); err != nil {
		t.Fatalf("CreatePoll: %v", err)
	}
	got, err := s.GetPoll(poll.ID)
	if err != nil {
		t.Fatalf("GetPoll: %v", err)
	}
	if len(got.Options) != 2 || got.Options[1] != "b" || got.IsClosed || got.MultipleChoice {
		t.Fatalf("poll = %+v", got)
	}
	if got.Votes == nil || got.Voters == nil {
		t.Fatalf("poll maps must not be nil: %+v", got)
	}
	for i := 0; i < 2; i++ {
		if err := s.VotePoll(poll.ID, "alice", 0); err != nil {
			t.Fatalf("VotePoll alice #%d: %v", i, err)
		}
	}
	if err := s.VotePoll(poll.ID, "bob", 0); err != nil {
		t.Fatalf("VotePoll bob: %v", err)
	}
	if err := s.VotePoll(poll.ID, "bob", 1); err != nil {
		t.Fatalf("VotePoll bob option 1: %v", err)
	}
	got, _ = s.GetPoll(poll.ID)
	if got.Votes[0] != 2 || got.Votes[1] != 1 {
		t.Fatalf("votes = %+v", got.Votes)
	}
	if len(got.Voters[0]) != 2 || got.Voters[0][0] != "alice" || got.Voters[0][1] != "bob" {
		t.Fatalf("voters = %+v", got.Voters)
	}
	if err := s.ClosePoll(poll.ID); err != nil {
		t.Fatalf("ClosePoll: %v", err)
	}
	got, _ = s.GetPoll(poll.ID)
	if !got.IsClosed {
		t.Fatal("poll not closed")
	}
}

func testCustomEmojis(t *testing.T, s Store) {
	if err := s.AddCustomEmoji("party_parrot", "https://example.com/p.gif", "alice", "room-e"); err != nil {
		t.Fatalf("AddCustomEmoji: %v", err)
	}
	if err := s.AddCustomEmoji("cat_jam", "https://example.com/c.gif", "bob", ""); err != nil {
		t.Fatalf("AddCustomEmoji global: %v", err)
	}
	room, err := s.ListCustomEmojis("room-e")
	if err != nil || len(room) != 2 {
		t.Fatalf("ListCustomEmojis room = %+v, err=%v", room, err)
	}
	other, err := s.ListCustomEmojis("other-room")
	if err != nil || len(other) != 1 || other[0].Name != "cat_jam" {
		t.Fatalf("ListCustomEmojis other = %+v, err=%v", other, err)
	}
	found, err := s.SearchCustomEmojis("PARTY")
	if err != nil || len(found) != 1 || found[0].Name != "party_parrot" {
		t.Fatalf("SearchCustomEmojis = %+v, err=%v", found, err)
	}
	if err := s.DeleteCustomEmoji("party_parrot", "bob"); err != nil {
		t.Fatalf("DeleteCustomEmoji wrong owner: %v", err)
	}
	if found, _ := s.SearchCustomEmojis("party"); len(found) != 1 {
		t.Fatal("non-owner deleted the emoji")
	}
	if err := s.DeleteCustomEmoji("party_parrot", "alice"); err != nil {
		t.Fatalf("DeleteCustomEmoji: %v", err)
	}
	if found, _ := s.SearchCustomEmojis("party"); len(found) != 0 {
		t.Fatal("emoji not deleted")
	}
}

func testExport(t *testing.T, s Store) {
	ctx := context.Background()
	room := "room-export-" + unique("x")
	a := publicMessage(t, s, "alice", room, "export one")
	// SQLite orders exports by timestamp only; keep the two rows in different
	// milliseconds so the assertion is not a same-millisecond coin flip.
	time.Sleep(2 * time.Millisecond)
	b := publicMessage(t, s, "bob", room, "export two")
	dm := laneMessage(t, s, "alice", "TokenBot", "", "dm export")
	deleted := publicMessage(t, s, "bob", room, "deleted export")
	if err := s.MarkDeleted(deleted.ID); err != nil {
		t.Fatalf("MarkDeleted: %v", err)
	}

	exported, err := s.ExportMessages(ctx, room, "", "", "", 0)
	if err != nil {
		t.Fatalf("ExportMessages room: %v", err)
	}
	if len(exported) != 2 || exported[0].ID != a.ID || exported[1].ID != b.ID {
		t.Fatalf("room export = %+v", exported)
	}
	dmExported, err := s.ExportMessages(ctx, "", "TokenBot", "", "alice", 0)
	if err != nil || len(dmExported) != 1 || dmExported[0].ID != dm.ID {
		t.Fatalf("dm export = %+v, err=%v", dmExported, err)
	}
	public, err := s.ExportMessages(ctx, "", "", "", "", 0)
	if err != nil {
		t.Fatalf("ExportMessages public: %v", err)
	}
	if _, ok := findMessage(public, dm.ID); ok {
		t.Fatal("public export leaked a DM")
	}
	if _, ok := findMessage(public, deleted.ID); ok {
		t.Fatal("public export included a deleted message")
	}
}

func testLocalAuth(t *testing.T, s Store) {
	code, err := s.GenerateInviteCode("admin", 1)
	if err != nil || code == "" {
		t.Fatalf("GenerateInviteCode = %q, err=%v", code, err)
	}
	if ok, err := s.ValidateInviteCode(strings.ToLower(code)); err != nil || !ok {
		t.Fatalf("ValidateInviteCode = %v, err=%v", ok, err)
	}
	if ok, err := s.ValidateInviteCode("NOPE"); err != nil || ok {
		t.Fatalf("unknown code = %v, err=%v", ok, err)
	}
	if err := s.RegisterUser("alice", "secret-password", code); err != nil {
		t.Fatalf("RegisterUser: %v", err)
	}
	if exists, err := s.UserExists("alice"); err != nil || !exists {
		t.Fatalf("UserExists = %v, err=%v", exists, err)
	}
	if ok, err := s.VerifyUser("alice", "secret-password"); err != nil || !ok {
		t.Fatalf("VerifyUser correct = %v, err=%v", ok, err)
	}
	if ok, err := s.VerifyUser("alice", "wrong"); err != nil || ok {
		t.Fatalf("VerifyUser wrong = %v, err=%v", ok, err)
	}
	if ok, err := s.VerifyUser("ghost", "whatever"); err != nil || ok {
		t.Fatalf("VerifyUser unknown = %v, err=%v", ok, err)
	}
	if ok, _ := s.ValidateInviteCode(code); ok {
		t.Fatal("exhausted invite code still valid")
	}
	if err := s.RegisterUser("bob", "another-password", code); err == nil {
		t.Fatal("exhausted invite code was accepted")
	}
	code2, err := s.GenerateInviteCode("admin", 5)
	if err != nil {
		t.Fatalf("GenerateInviteCode 2: %v", err)
	}
	if err := s.RegisterUser("alice", "another-password", code2); err == nil {
		t.Fatal("duplicate username was accepted")
	}
	codes, err := s.ListInviteCodes("admin")
	if err != nil || len(codes) != 2 {
		t.Fatalf("ListInviteCodes = %+v, err=%v", codes, err)
	}
}

func testOIDCIdentity(t *testing.T, s Store) {
	username, err := s.UpsertOIDCUser(store.OIDCUserProfile{
		Sub:               "sub-1",
		DesiredUsername:   "alice",
		Email:             "alice@example.com",
		PreferredUsername: "alice",
		Name:              "Alice",
		Picture:           "https://example.com/a.png",
	})
	if err != nil || username != "alice" {
		t.Fatalf("UpsertOIDCUser = %q, err=%v", username, err)
	}
	got, err := s.GetOIDCUserBySub("sub-1")
	if err != nil || got.ChatUsername != "alice" || got.CreatedAt == 0 || got.UpdatedAt == 0 {
		t.Fatalf("GetOIDCUserBySub = %+v, err=%v", got, err)
	}
	username, err = s.UpsertOIDCUser(store.OIDCUserProfile{
		Sub: "sub-1", DesiredUsername: "ignored", Email: "new@example.com", Name: "Alice Updated",
	})
	if err != nil || username != "alice" {
		t.Fatalf("UpsertOIDCUser update = %q, err=%v", username, err)
	}
	if got, _ := s.GetOIDCUserBySub("sub-1"); got.Email != "new@example.com" || got.Name != "Alice Updated" {
		t.Fatalf("identity update = %+v", got)
	}
	second, err := s.UpsertOIDCUser(store.OIDCUserProfile{Sub: "sub-2", DesiredUsername: "alice"})
	if err != nil {
		t.Fatalf("UpsertOIDCUser second: %v", err)
	}
	if second == "" || second == "alice" {
		t.Fatalf("colliding username = %q", second)
	}
	if got, err := s.GetOIDCUserByUsername(second); err != nil || got.Sub != "sub-2" {
		t.Fatalf("GetOIDCUserByUsername = %+v, err=%v", got, err)
	}

	if gen, err := s.OIDCLogoutGeneration("sub-1"); err != nil || gen != 0 {
		t.Fatalf("initial generation = %d, err=%v", gen, err)
	}
	now := time.Now()
	result, err := s.ApplyOIDCBackchannelLogout("sub-1", "jti-1", "hash-1", now, now.Add(time.Hour))
	if err != nil || result.Generation != 1 || result.Duplicate || result.ChatUsername != "alice" {
		t.Fatalf("logout = %+v, err=%v", result, err)
	}
	again, err := s.ApplyOIDCBackchannelLogout("sub-1", "jti-1", "hash-1", now, now.Add(time.Hour))
	if err != nil || !again.Duplicate || again.Generation != 1 {
		t.Fatalf("duplicate logout = %+v, err=%v", again, err)
	}
	if _, err := s.ApplyOIDCBackchannelLogout("sub-1", "jti-1", "other-hash", now, now.Add(time.Hour)); !errors.Is(err, store.ErrOIDCLogoutJTIReuse) {
		t.Fatalf("JTI reuse err = %v", err)
	}
	next, err := s.ApplyOIDCBackchannelLogout("sub-1", "jti-2", "hash-2", now, now.Add(time.Hour))
	if err != nil || next.Generation != 2 {
		t.Fatalf("second logout = %+v, err=%v", next, err)
	}
	if got, _ := s.OIDCLogoutGeneration("sub-1"); got != 2 {
		t.Fatalf("generation = %d, want 2", got)
	}
	if _, err := s.ApplyOIDCBackchannelLogout("", "jti", "hash", now, now.Add(time.Hour)); err == nil {
		t.Fatal("empty subject accepted")
	}
}
