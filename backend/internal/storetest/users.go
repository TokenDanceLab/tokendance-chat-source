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

// testGuestIdentities pins the bind-once contract: a browser's cookie owns the
// first handle it joins with, and no other cookie can take that handle.
func testGuestIdentities(t *testing.T, s Store) {
	t.Helper()

	if handle, ok := s.GuestHandle("guest-a"); ok || handle != "" {
		t.Fatalf("unbound GuestHandle = %q, %v", handle, ok)
	}

	if _, err := s.ResolveGuestIdentity("", "alice"); err == nil {
		t.Fatal("empty guest id was accepted")
	}
	if _, err := s.ResolveGuestIdentity("guest-a", ""); err == nil {
		t.Fatal("empty handle was accepted")
	}

	bound, err := s.ResolveGuestIdentity("guest-a", "alice")
	if err != nil || bound != "alice" {
		t.Fatalf("first bind = %q, %v", bound, err)
	}
	// Idempotent re-join from another tab of the same browser.
	if bound, err := s.ResolveGuestIdentity("guest-a", "alice"); err != nil || bound != "alice" {
		t.Fatalf("same browser re-join = %q, %v", bound, err)
	}
	// The same cookie cannot silently switch handles.
	if bound, err := s.ResolveGuestIdentity("guest-a", "bob"); !errors.Is(err, store.ErrGuestHandleMismatch) || bound != "alice" {
		t.Fatalf("handle switch = %q, %v", bound, err)
	}
	// Another cookie cannot claim a bound handle.
	if _, err := s.ResolveGuestIdentity("guest-b", "alice"); !errors.Is(err, store.ErrGuestHandleTaken) {
		t.Fatalf("foreign claim error = %v", err)
	}
	if bound, err := s.ResolveGuestIdentity("guest-b", "bob"); err != nil || bound != "bob" {
		t.Fatalf("second browser = %q, %v", bound, err)
	}

	if handle, ok := s.GuestHandle("guest-a"); !ok || handle != "alice" {
		t.Fatalf("GuestHandle = %q, %v", handle, ok)
	}
	// A handle that already owns private history must stay sealed even when no
	// binding row exists (identities created before cookie binding).
	laneMessage(t, s, "legacy_guest", "TokenBot", "", "legacy private")
	if _, err := s.ResolveGuestIdentity("guest-new", "legacy_guest"); !errors.Is(err, store.ErrGuestHandleTaken) {
		t.Fatalf("claim of handle with history = %v, want ErrGuestHandleTaken", err)
	}
	// A pre-signature cookie is accepted once so its original browser keeps
	// access; binding is stored under the plain cookie ID.
	if bound, err := s.ResolveGuestIdentity("legacy:guest-old-cookie", "legacy_guest"); err != nil || bound != "legacy_guest" {
		t.Fatalf("legacy cookie upgrade = %q, %v", bound, err)
	}
	if handle, ok := s.GuestHandle("guest-old-cookie"); !ok || handle != "legacy_guest" {
		t.Fatalf("legacy binding stored under prefixed id: %q, %v", handle, ok)
	}
	if bound, err := s.ResolveGuestIdentity("guest-old-cookie", "legacy_guest"); err != nil || bound != "legacy_guest" {
		t.Fatalf("signed cookie after legacy upgrade = %q, %v", bound, err)
	}
	if handle, ok := s.GuestHandle("missing"); ok || handle != "" {
		t.Fatalf("missing GuestHandle = %q, %v", handle, ok)
	}
}

// testAgentMemories pins the explicit-memory contract: scope isolation, exact
// dedupe, supersede inside one transaction, and owner-only deletion.
func testAgentMemories(t *testing.T, s Store) {
	t.Helper()
	const actor = "oidc:sub-memory"
	base := store.AgentMemory{
		OwnerActorID: actor,
		Scope:        "lane",
		ThreadID:     "lane-1",
		Kind:         "preference",
		Content:      "用户 对  花生过敏",
	}

	first, created, err := s.CreateAgentMemory(base, "")
	if err != nil || !created {
		t.Fatalf("create = %v, %v", created, err)
	}
	if first.Content != "用户 对 花生过敏" || first.ContentHash != store.MemoryContentHash("用户 对 花生过敏") {
		t.Fatalf("normalised memory = %+v", first)
	}
	if first.ID == "" || first.Status != "active" {
		t.Fatalf("created memory = %+v", first)
	}

	dup, createdAgain, err := s.CreateAgentMemory(base, "")
	if err != nil || createdAgain || dup.ID != first.ID {
		t.Fatalf("duplicate = %+v, %v, %v", dup, createdAgain, err)
	}

	if got := s.ListAgentMemories(actor, "lane-1", 20); len(got) != 1 || got[0].ID != first.ID {
		t.Fatalf("lane list = %+v", got)
	}
	if got := s.ListAgentMemories(actor, "lane-2", 20); len(got) != 0 {
		t.Fatalf("lane memory leaked into another lane: %+v", got)
	}
	if got := s.ListAgentMemories("oidc:other", "lane-1", 20); len(got) != 0 {
		t.Fatalf("memory leaked to another actor: %+v", got)
	}

	actorWide, created, err := s.CreateAgentMemory(store.AgentMemory{
		OwnerActorID: actor, Scope: "actor", Kind: "project", Content: "正在做 PG 迁移",
	}, "")
	if err != nil || !created {
		t.Fatalf("actor memory = %v, %v", created, err)
	}
	if got := s.ListAgentMemories(actor, "lane-2", 20); len(got) != 1 || got[0].ID != actorWide.ID {
		t.Fatalf("actor-wide memory not visible in another lane: %+v", got)
	}

	corrected, created, err := s.CreateAgentMemory(store.AgentMemory{
		OwnerActorID: actor, Scope: "lane", ThreadID: "lane-1", Kind: "preference", Content: "用户对花生严重过敏",
	}, first.ID)
	if err != nil || !created {
		t.Fatalf("supersede = %v, %v", created, err)
	}
	lane := s.ListAgentMemories(actor, "lane-1", 20)
	foundCorrected := false
	for _, m := range lane {
		if m.ID == first.ID {
			t.Fatalf("superseded memory still active: %+v", lane)
		}
		if m.ID == corrected.ID {
			foundCorrected = true
		}
	}
	if !foundCorrected {
		t.Fatalf("corrected memory missing: %+v", lane)
	}

	if _, _, err := s.CreateAgentMemory(store.AgentMemory{
		OwnerActorID: "oidc:other", Scope: "lane", ThreadID: "lane-1", Kind: "preference", Content: "别人的事实",
	}, first.ID); err == nil {
		t.Fatal("foreign supersede was accepted")
	}

	if deleted, err := s.DeleteAgentMemory("oidc:other", corrected.ID); err != nil || deleted {
		t.Fatalf("foreign delete = %v, %v", deleted, err)
	}
	if deleted, err := s.DeleteAgentMemory(actor, corrected.ID); err != nil || !deleted {
		t.Fatalf("delete = %v, %v", deleted, err)
	}
	for _, m := range s.ListAgentMemories(actor, "lane-1", 20) {
		if m.ID == corrected.ID {
			t.Fatal("deleted memory still listed")
		}
	}
}

// testMaintenanceSweeps covers the background cleanup primitives.
func testMaintenanceSweeps(t *testing.T, s Store) {
	t.Helper()
	if _, err := s.ResolveGuestIdentity("guest-old", "old_handle"); err != nil {
		t.Fatal(err)
	}
	if removed, err := s.PurgeGuestIdentities(time.Now().Add(time.Hour)); err != nil || removed == 0 {
		t.Fatalf("purge = %d, %v", removed, err)
	}
	if _, ok := s.GuestHandle("guest-old"); ok {
		t.Fatal("purged guest binding is still present")
	}

	expires := time.Now().Add(-time.Minute).UnixMilli()
	if _, _, err := s.CreateAgentMemory(store.AgentMemory{
		OwnerActorID: "oidc:sub-purge", Scope: "lane", ThreadID: "lane-1",
		Kind: "event", Content: "临时事件", ExpiresAt: &expires,
	}, ""); err != nil {
		t.Fatal(err)
	}
	if expired, err := s.ExpireAgentMemories(time.Now()); err != nil || expired == 0 {
		t.Fatalf("expire = %d, %v", expired, err)
	}
	if got := s.ListAgentMemories("oidc:sub-purge", "lane-1", 20); len(got) != 0 {
		t.Fatalf("expired memory still listed: %+v", got)
	}

	if _, err := s.InsertMessage("alice", "see ![图片](/uploads/images/keep.png)", "", "room-1", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if referenced, err := s.IsUploadReferenced("keep.png"); err != nil || !referenced {
		t.Fatalf("referenced upload = %v, %v", referenced, err)
	}
	if referenced, err := s.IsUploadReferenced("orphan.png"); err != nil || referenced {
		t.Fatalf("orphan upload = %v, %v", referenced, err)
	}
}
