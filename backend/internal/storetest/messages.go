package storetest

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"tokendancechat/backend/store"
)

func testMessages(t *testing.T, s Store) {
	room := "room-" + unique("m")
	first := publicMessage(t, s, "alice", room, "first")
	time.Sleep(2 * time.Millisecond)
	second := publicMessage(t, s, "bob", room, "second")
	time.Sleep(2 * time.Millisecond)
	third := publicMessage(t, s, "alice", room, "third")

	recent := s.GetMessages(2, 0)
	if len(recent) != 2 {
		t.Fatalf("GetMessages(2) = %d, want 2", len(recent))
	}
	if recent[0].ID != second.ID || recent[1].ID != third.ID {
		t.Fatalf("GetMessages order = [%s %s], want [%s %s]", recent[0].ID, recent[1].ID, second.ID, third.ID)
	}
	older := s.GetMessages(10, third.Timestamp)
	if _, ok := findMessage(older, third.ID); ok {
		t.Fatal("before boundary must be exclusive")
	}
	if _, ok := findMessage(older, first.ID); !ok {
		t.Fatal("older page lost the first message")
	}

	edited, err := s.UpdateMessage(first.ID, "first-edited")
	if err != nil {
		t.Fatalf("UpdateMessage: %v", err)
	}
	if edited.Content != "first-edited" || !edited.Edited {
		t.Fatalf("edited message = %+v", edited)
	}

	if err := s.MarkDeleted(second.ID); err != nil {
		t.Fatalf("MarkDeleted: %v", err)
	}
	deleted, err := s.GetMessageByID(second.ID)
	if err != nil {
		t.Fatalf("GetMessageByID(deleted): %v", err)
	}
	if !deleted.Deleted || deleted.Content != "" {
		t.Fatalf("deleted message exposed content: %+v", deleted)
	}
	roomMessages := s.GetRoomMessages(room, 10, 0)
	if m, ok := findMessage(roomMessages, second.ID); !ok || m.Content != "" {
		t.Fatalf("deleted message must stay visible but content-free: %+v", roomMessages)
	}

	// Parts: reasoning survives in a private message and is projected away in
	// the public one. Both inserts pass through WithoutSourcesJSON.
	parts := json.RawMessage(`[{"type":"text","text":"visible"},{"type":"reasoning","text":"private-thought"}]`)
	private := insertParts(t, s, "TokenBot", "visible", "", "alice", "", parts)
	if !strings.Contains(string(private.Parts), "reasoning") {
		t.Fatalf("private parts lost reasoning: %s", private.Parts)
	}
	storedPrivate, err := s.GetMessageByID(private.ID)
	if err != nil || !strings.Contains(string(storedPrivate.Parts), "reasoning") {
		t.Fatalf("stored private parts = %s, err=%v", storedPrivate.Parts, err)
	}
	pub := insertParts(t, s, "TokenBot", "visible", "", "", "", parts)
	if strings.Contains(string(pub.Parts), "reasoning") {
		t.Fatalf("public parts leaked reasoning: %s", pub.Parts)
	}
	storedPub, err := s.GetMessageByID(pub.ID)
	if err != nil || strings.Contains(string(storedPub.Parts), "reasoning") {
		t.Fatalf("stored public parts = %s, err=%v", storedPub.Parts, err)
	}

	actorMessage := publicMessage(t, s, "carol", room, "actor-bound")
	gotActor, err := s.GetMessageByID(actorMessage.ID)
	if err != nil || gotActor.ActorID != "local:carol" {
		t.Fatalf("actor_id = %q, err=%v", gotActor.ActorID, err)
	}

	if got := s.TotalMessages(); got < 4 {
		t.Fatalf("TotalMessages = %d, want >= 4", got)
	}
	if err := s.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if users := s.TotalUsers(); users != 0 {
		t.Fatalf("TotalUsers = %d, want 0 before registration", users)
	}
	base, err := s.InsertMessage("alice", "base message", "", room, "", "", "")
	if err != nil || base.ID == "" || base.Content != "base message" {
		t.Fatalf("InsertMessage = %+v, err=%v", base, err)
	}
}

func testPrivateLanes(t *testing.T, s Store) {
	mainOne := laneMessage(t, s, "alice", "TokenBot", "", "main one")
	mainReply := insertActor(t, s, "bot:tokenbot", "TokenBot", "main reply", "", "", "alice", "")
	laneAOne := laneMessage(t, s, "alice", "TokenBot", "lane-a", "side one")
	laneATwo := laneMessage(t, s, "alice", "TokenBot", "lane-a", "side two")
	other := laneMessage(t, s, "bob", "TokenBot", "", "bob main")

	main := s.GetMessagesBetween("alice", "TokenBot", 10)
	if len(main) != 2 || main[0].ID != mainOne.ID || main[1].ID != mainReply.ID {
		t.Fatalf("main lane = %+v", main)
	}
	for _, m := range main {
		if m.ID == other.ID {
			t.Fatal("another user's DM leaked into the lane")
		}
	}
	lane := s.GetMessagesBetweenThread("alice", "TokenBot", "lane-a", 10)
	if len(lane) != 2 || lane[0].ID != laneAOne.ID || lane[1].ID != laneATwo.ID {
		t.Fatalf("side lane = %+v", lane)
	}

	if _, err := s.ToggleReaction(mainOne.ID, "👍", "alice"); err != nil {
		t.Fatalf("reaction on DM: %v", err)
	}
	main = s.GetMessagesBetween("alice", "TokenBot", 10)
	if len(main) != 2 || len(main[0].Reactions["👍"]) != 1 {
		t.Fatalf("lane reactions not enriched: %+v", main)
	}

	threads := s.ListPrivateThreads("alice", "TokenBot")
	if len(threads) != 2 {
		t.Fatalf("ListPrivateThreads = %+v", threads)
	}
	if threads[0].ThreadID != "" || threads[0].Count != 2 || threads[0].Preview != "main one" || threads[0].LastAt == 0 {
		t.Fatalf("main thread summary = %+v", threads[0])
	}
	if threads[1].ThreadID != "lane-a" || threads[1].Count != 2 {
		t.Fatalf("side thread summary = %+v", threads[1])
	}
}

func testRooms(t *testing.T, s Store) {
	aID, err := s.CreateRoom("room-a-" + unique("x"))
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	zID, err := s.CreateRoom("room-z-" + unique("x"))
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if got, err := s.GetRoomID("room-z-" + unique("none")); err == nil {
		t.Fatalf("GetRoomID(unknown) = %q, want error", got)
	}
	rooms := s.ListRooms()
	if len(rooms) < 2 {
		t.Fatalf("ListRooms = %+v", rooms)
	}
	var aIndex, zIndex = -1, -1
	for i, r := range rooms {
		switch r.ID {
		case aID:
			aIndex = i
		case zID:
			zIndex = i
		}
	}
	if aIndex < 0 || zIndex < 0 || aIndex > zIndex {
		t.Fatalf("rooms not ordered by name: %+v", rooms)
	}
	if err := s.DeleteRoom(aID); err != nil {
		t.Fatalf("DeleteRoom: %v", err)
	}
	for _, r := range s.ListRooms() {
		if r.ID == aID {
			t.Fatal("deleted room still listed")
		}
	}
}

func testReactions(t *testing.T, s Store) {
	msg := publicMessage(t, s, "alice", "room-r", "react to me")
	got, err := s.ToggleReaction(msg.ID, "👍", "alice")
	if err != nil {
		t.Fatalf("ToggleReaction add: %v", err)
	}
	if len(got["👍"]) != 1 || got["👍"][0] != "alice" {
		t.Fatalf("add result = %+v", got)
	}
	got, err = s.ToggleReaction(msg.ID, "👍", "bob")
	if err != nil {
		t.Fatalf("ToggleReaction second: %v", err)
	}
	if len(got["👍"]) != 2 || got["👍"][0] != "alice" || got["👍"][1] != "bob" {
		t.Fatalf("insertion order lost: %+v", got)
	}
	got, err = s.ToggleReaction(msg.ID, "👍", "alice")
	if err != nil {
		t.Fatalf("ToggleReaction remove: %v", err)
	}
	if len(got["👍"]) != 1 || got["👍"][0] != "bob" {
		t.Fatalf("remove result = %+v", got)
	}
	batch := s.GetReactionsForMessages([]string{msg.ID, "missing"})
	if len(batch[msg.ID]["👍"]) != 1 {
		t.Fatalf("batch reactions = %+v", batch)
	}
	if batch["missing"] == nil {
		t.Fatal("batch reactions must pre-fill empty maps")
	}
}

func testSearch(t *testing.T, s Store) {
	room1 := "room-search-" + unique("1")
	room2 := "room-search-" + unique("2")
	hello1 := publicMessage(t, s, "alice", room1, "hello world")
	hello2 := publicMessage(t, s, "bob", room1, "你好世界 hello")
	unicorn := publicMessage(t, s, "carol", room2, "unicorn rainbow")
	dmAlice := laneMessage(t, s, "alice", "TokenBot", "", "unicorn secret")
	dmBob := laneMessage(t, s, "bob", "TokenBot", "", "unicorn other")

	results, err := s.SearchMessages("hello", "", 10)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("public search hello = %+v", results)
	}
	for _, r := range results {
		if r.Snippet == "" {
			t.Fatalf("empty snippet for %q", r.Content)
		}
	}
	if r, ok := findResult(results, hello1.ID); !ok || !strings.Contains(r.Content, "hello") {
		t.Fatalf("hello1 missing: %+v", results)
	}
	if _, ok := findResult(results, hello2.ID); !ok {
		t.Fatalf("hello2 missing: %+v", results)
	}

	roomResults, err := s.SearchMessages("hello", room1, 10)
	if err != nil || len(roomResults) != 2 {
		t.Fatalf("room-scoped search = %+v, err=%v", roomResults, err)
	}
	if _, err := s.SearchMessages("hello", room2, 10); err != nil {
		t.Fatalf("room-scoped search: %v", err)
	}
	if got, _ := s.SearchMessages("hello", room2+"-other", 10); len(got) != 0 {
		t.Fatalf("search leaked across rooms: %+v", got)
	}

	publicUnicorn, err := s.SearchMessages("unicorn", "", 10)
	if err != nil || len(publicUnicorn) != 1 || publicUnicorn[0].ID != unicorn.ID {
		t.Fatalf("public unicorn = %+v, err=%v", publicUnicorn, err)
	}
	privateAlice, err := s.SearchPrivateMessages("alice", "TokenBot", "", "unicorn", 10)
	if err != nil || len(privateAlice) != 1 || privateAlice[0].ID != dmAlice.ID {
		t.Fatalf("private alice = %+v, err=%v", privateAlice, err)
	}
	privateBob, err := s.SearchPrivateMessages("bob", "TokenBot", "", "unicorn", 10)
	if err != nil || len(privateBob) != 1 || privateBob[0].ID != dmBob.ID {
		t.Fatalf("private bob = %+v, err=%v", privateBob, err)
	}
	// A side lane is a separate conversation: a search from lane-a must not
	// surface main-lane history.
	sideAlice := laneMessage(t, s, "alice", "TokenBot", "lane-a", "unicorn side")
	sideHits, err := s.SearchPrivateMessages("alice", "TokenBot", "lane-a", "unicorn", 10)
	if err != nil || len(sideHits) != 1 || sideHits[0].ID != sideAlice.ID {
		t.Fatalf("lane-scoped private search = %+v, err=%v", sideHits, err)
	}

	if err := s.MarkDeleted(unicorn.ID); err != nil {
		t.Fatalf("MarkDeleted: %v", err)
	}
	if got, _ := s.SearchMessages("unicorn", "", 10); len(got) != 0 {
		t.Fatalf("deleted message still searchable: %+v", got)
	}
	if got, err := s.SearchMessages("   ", "", 10); err != nil || len(got) != 0 {
		t.Fatalf("blank query = %+v, err=%v", got, err)
	}
	if got, err := s.SearchMessages("%%%", "", 10); err != nil || len(got) != 0 {
		t.Fatalf("operator-only query = %+v, err=%v", got, err)
	}
	// CJK behaviour is tokenizer-dependent in SQLite; both implementations must
	// only promise not to error.
	if _, err := s.SearchMessages("你好", "", 10); err != nil {
		t.Fatalf("CJK search error: %v", err)
	}
}

func findResult(results []store.SearchResult, id string) (store.SearchResult, bool) {
	for _, r := range results {
		if r.ID == id {
			return r, true
		}
	}
	return store.SearchResult{}, false
}

func testPinsBlocksPrefs(t *testing.T, s Store) {
	msg := publicMessage(t, s, "alice", "room-pin", "pin me")
	for i := 0; i < 2; i++ {
		if err := s.PinMessage("room-pin", msg.ID, "alice"); err != nil {
			t.Fatalf("PinMessage #%d: %v", i, err)
		}
	}
	pinned := s.GetPinnedMessages("room-pin")
	if len(pinned) != 1 || pinned[0].ID != msg.ID {
		t.Fatalf("pins = %+v", pinned)
	}
	if err := s.UnpinMessage("room-pin", msg.ID); err != nil {
		t.Fatalf("UnpinMessage: %v", err)
	}
	if len(s.GetPinnedMessages("room-pin")) != 0 {
		t.Fatal("unpinned message still listed")
	}

	if err := s.BlockUser("alice", "bob"); err != nil {
		t.Fatalf("BlockUser: %v", err)
	}
	if err := s.BlockUser("alice", "bob"); err != nil {
		t.Fatalf("BlockUser idempotency: %v", err)
	}
	if !s.IsBlocked("alice", "bob") {
		t.Fatal("IsBlocked = false")
	}
	if blocked := s.GetBlockedUsers("alice"); len(blocked) != 1 || blocked[0] != "bob" {
		t.Fatalf("GetBlockedUsers = %+v", blocked)
	}
	if err := s.UnblockUser("alice", "bob"); err != nil {
		t.Fatalf("UnblockUser: %v", err)
	}
	if s.IsBlocked("alice", "bob") {
		t.Fatal("IsBlocked after unblock = true")
	}
	if got := s.GetBlockedUsers("nobody"); got == nil || len(got) != 0 {
		t.Fatalf("empty block list = %#v", got)
	}

	if muted, preview, err := s.GetNotificationPrefs("alice", "dm:bob"); err != nil || muted != 0 || !preview {
		t.Fatalf("default prefs = (%d,%v,%v)", muted, preview, err)
	}
	if err := s.SetNotificationPrefs("alice", "dm:bob", 5000, false); err != nil {
		t.Fatalf("SetNotificationPrefs: %v", err)
	}
	if muted, preview, err := s.GetNotificationPrefs("alice", "dm:bob"); err != nil || muted != 5000 || preview {
		t.Fatalf("stored prefs = (%d,%v,%v)", muted, preview, err)
	}
	prefs := s.ListNotificationPrefs("alice")
	if len(prefs) != 1 || prefs[0].Key != "dm:bob" {
		t.Fatalf("ListNotificationPrefs = %+v", prefs)
	}
}

func testThreads(t *testing.T, s Store) {
	room := "room-thread-" + unique("p")
	parent := publicMessage(t, s, "alice", room, "parent")
	reply1 := insertActor(t, s, "local:bob", "bob", "reply one", "", room, "", parent.ID)
	time.Sleep(2 * time.Millisecond)
	reply2 := insertActor(t, s, "local:carol", "carol", "reply two", "", room, "", parent.ID)
	// Same thread id in another room must not join this thread.
	foreign := insertActor(t, s, "local:bob", "bob", "foreign", "", "other-room", "", parent.ID)
	deletedReply := insertActor(t, s, "local:bob", "bob", "deleted", "", room, "", parent.ID)
	if err := s.MarkDeleted(deletedReply.ID); err != nil {
		t.Fatalf("MarkDeleted: %v", err)
	}

	thread := s.GetThreadMessages(parent.ID)
	if len(thread) != 2 || thread[0].ID != reply1.ID || thread[1].ID != reply2.ID {
		t.Fatalf("thread = %+v", thread)
	}
	if _, ok := findMessage(thread, foreign.ID); ok {
		t.Fatal("cross-room reply leaked into thread")
	}
	if _, ok := findMessage(thread, deletedReply.ID); ok {
		t.Fatal("deleted reply leaked into thread")
	}
	if got := s.GetThreadReplyCount(parent.ID); got != 2 {
		t.Fatalf("reply count = %d, want 2", got)
	}

	if _, err := s.ToggleReaction(reply1.ID, "🎉", "alice"); err != nil {
		t.Fatalf("thread reaction: %v", err)
	}
	thread = s.GetThreadMessages(parent.ID)
	if len(thread) != 2 || len(thread[0].Reactions["🎉"]) != 1 {
		t.Fatalf("thread reactions not enriched: %+v", thread)
	}

	privateParent := laneMessage(t, s, "alice", "TokenBot", "", "private parent")
	privateReply := insertActor(t, s, "bot:tokenbot", "TokenBot", "private reply", "", "", "alice", privateParent.ID)
	outsider := insertActor(t, s, "local:bob", "bob", "outsider", "", "", "TokenBot", privateParent.ID)
	privateThread := s.GetThreadMessages(privateParent.ID)
	if len(privateThread) != 1 || privateThread[0].ID != privateReply.ID {
		t.Fatalf("private thread = %+v", privateThread)
	}
	if _, ok := findMessage(privateThread, outsider.ID); ok {
		t.Fatal("outsider joined a private thread")
	}

	if err := s.MarkDeleted(parent.ID); err != nil {
		t.Fatalf("MarkDeleted parent: %v", err)
	}
	if got := s.GetThreadMessages(parent.ID); len(got) != 0 {
		t.Fatalf("deleted parent thread = %+v", got)
	}
	if got := s.GetThreadReplyCount(parent.ID); got != 0 {
		t.Fatalf("deleted parent count = %d", got)
	}
}
