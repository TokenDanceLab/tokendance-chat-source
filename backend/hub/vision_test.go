package hub

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tokendancechat/backend/store"
)

const (
	testPNGName  = "11111111-1111-1111-1111-111111111111.png"
	testPNGRef   = "/uploads/images/" + testPNGName
	testJPEGName = "22222222-2222-2222-2222-222222222222.jpg"
	testJPEGRef  = "/uploads/images/" + testJPEGName
)

// writeUploadPNG writes a real, decodable PNG under <uploadsDir>/images and
// returns its bytes.
func writeUploadPNG(t *testing.T, uploadsDir, name string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(uploadsDir, "images")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractImageRefs(t *testing.T) {
	const second = "/uploads/images/USER-2.JPEG"
	cases := []struct {
		name string
		in   string
		text string
		refs []string
	}{
		{"plain text", "hello there", "hello there", nil},
		{"single image", "look ![图](" + testPNGRef + ")", "look [图片]", []string{testPNGRef}},
		{
			"multiple images keep one placeholder each",
			"![a](" + testPNGRef + ") and ![b](" + second + ")",
			"[图片] and [图片]",
			[]string{testPNGRef, second},
		},
		{
			"duplicate refs yield one entry",
			"![a](" + testPNGRef + ") ![b](" + testPNGRef + ")",
			"[图片] [图片]",
			[]string{testPNGRef},
		},
		{"traversal path is not an image ref", "![x](/uploads/images/../secret.png)", "![x](/uploads/images/../secret.png)", nil},
		{"absolute URL is not an image ref", "![x](https://example.com/uploads/images/a.png)", "![x](https://example.com/uploads/images/a.png)", nil},
		{"non-image extension is not an image ref", "![x](/uploads/images/a.gif)", "![x](/uploads/images/a.gif)", nil},
		{"emoji path is not an image ref", "![x](/uploads/emojis/a.png)", "![x](/uploads/emojis/a.png)", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, refs := extractImageRefs(tc.in)
			if text != tc.text {
				t.Errorf("text = %q, want %q", text, tc.text)
			}
			if strings.Join(refs, ",") != strings.Join(tc.refs, ",") {
				t.Errorf("refs = %v, want %v", refs, tc.refs)
			}
		})
	}
}

func TestLoadImageDataURL(t *testing.T) {
	root := t.TempDir()
	data := writeUploadPNG(t, root, testPNGName)

	got, _, err := loadImageDataURL(root, testPNGRef, maxRunImageBytes)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
	if got != want {
		t.Errorf("data URL does not match image bytes")
	}

	// Uppercase extensions are accepted and choose the JPEG mime type.
	if err := os.WriteFile(filepath.Join(root, "images", testJPEGName), []byte{0xFF, 0xD8, 0xFF}, 0644); err != nil {
		t.Fatal(err)
	}
	if got, _, err := loadImageDataURL(root, testJPEGRef, maxRunImageBytes); err != nil || !strings.HasPrefix(got, "data:image/jpeg;base64,") {
		t.Errorf("jpeg data URL = %q, err = %v", got, err)
	}
}

func TestLoadImageDataURLRejectsUnsafeOrInvalidRefs(t *testing.T) {
	root := t.TempDir()
	writeUploadPNG(t, root, testPNGName)

	cases := []struct {
		name string
		root string
		ref  string
	}{
		{"traversal escaping images", root, "/uploads/images/../../secret.png"},
		{"absolute path", root, "/etc/passwd.png"},
		{"emoji prefix", root, "/uploads/emojis/" + testPNGName},
		{"non-image extension", root, "/uploads/images/a.gif"},
		{"bare file name", root, testPNGName},
		{"missing file", root, "/uploads/images/missing.png"},
		{"unconfigured root", "", testPNGRef},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := loadImageDataURL(tc.root, tc.ref, maxRunImageBytes); err == nil {
				t.Fatalf("load(%q) succeeded, want error", tc.ref)
			}
		})
	}
}

func TestLoadImageDataURLRejectsOversizedAndNonRegularFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "images")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(dir, "big.png")
	if err := os.WriteFile(big, make([]byte, maxRunImageBytes+1), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadImageDataURL(root, "/uploads/images/big.png", maxRunImageBytes); err == nil {
		t.Fatal("oversized image was accepted")
	}
	if err := os.MkdirAll(filepath.Join(dir, "folder.png"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadImageDataURL(root, "/uploads/images/folder.png", maxRunImageBytes); err == nil {
		t.Fatal("directory was accepted as an image")
	}
}

// enqueueVisionRun enables vision on a fresh test hub and returns the hub,
// store, model and uploads root.
func enqueueVisionRun(t *testing.T) (*Hub, *store.Store, *controlledModel, string) {
	t.Helper()
	h, s, model := runTestHub(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "images"), 0755); err != nil {
		t.Fatal(err)
	}
	h.uploadsDir = root
	h.visionEnabled = true
	return h, s, model, root
}

func TestEnqueueRunAttachesNewestImagesWithinLimit(t *testing.T) {
	h, s, model, root := enqueueVisionRun(t)
	room := h.DefaultRoomID()
	alice := runClient(h, "alice", "account-A", room)

	names := []string{"old-1.png", "old-2.png", "old-3.png", "newest.png"}
	payload := make(map[string][]byte, len(names))
	for _, name := range names {
		payload[name] = writeUploadPNG(t, root, name)
	}
	// A bot-authored message with an image must stay text-only.
	if _, err := s.InsertMessage(h.botName, "earlier ![图](/uploads/images/old-1.png)", "", room, "", "", ""); err != nil {
		t.Fatal(err)
	}
	for _, name := range names[:3] {
		if _, err := s.InsertMessage("alice", "look ![图](/uploads/images/"+name+")", "", room, "", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	trigger, err := s.InsertMessage("alice", "@TokenBot ![图](/uploads/images/"+names[3]+")", "", room, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	h.enqueueRun(alice, trigger, false)
	call := nextCall(t, model)

	if len(call.input) != 5 {
		t.Fatalf("input rows = %d, want 5 (the trigger appears once)", len(call.input))
	}
	// Rows: bot text, old-1, old-2, old-3, trigger.
	if got := call.input[0]; got.Role != "assistant" || len(got.Images) != 0 || !strings.Contains(got.Content, "[图片]") {
		t.Errorf("bot row = %+v", got)
	}
	if got := call.input[1]; len(got.Images) != 0 || !strings.Contains(got.Content, "[图片]") {
		t.Errorf("oldest image row should be placeholder-only after the 3-image budget: %+v", got)
	}
	for i := 2; i <= 3; i++ {
		if len(call.input[i].Images) != 1 {
			t.Errorf("row %d images = %d, want 1", i, len(call.input[i].Images))
		}
	}
	// The frozen trigger is rendered once: the snapshot copy is skipped so it
	// cannot consume image budget or repeat the request text.
	joined := ""
	for _, row := range call.input {
		joined += row.Content + "\n"
	}
	if strings.Count(joined, trigger.ID) != 1 {
		t.Errorf("trigger id appears %d times in the run input", strings.Count(joined, trigger.ID))
	}
	last := call.input[4]
	if len(last.Images) != 1 {
		t.Fatalf("trigger images = %d, want 1", len(last.Images))
	}
	wantURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(payload[names[3]])
	if last.Images[0] != wantURL {
		t.Errorf("trigger image is not the newest upload")
	}
	if !strings.Contains(last.Content, trigger.ID) || !strings.Contains(last.Content, "[图片]") {
		t.Errorf("trigger content = %q", last.Content)
	}

	finishCall(call, "done")
	rows, _ := s.RecentBotRuns(room, alice.actorID(), 1)
	if len(rows) != 1 {
		t.Fatalf("runs = %d", len(rows))
	}
	awaitStatus(t, s, rows[0].ID, "completed")
}

func TestEnqueueRunStopsAtTotalImageBudget(t *testing.T) {
	h, s, model, root := enqueueVisionRun(t)
	room := h.DefaultRoomID()
	alice := runClient(h, "alice", "account-A", room)

	// Three images under the per-image cap but over the per-run total: the
	// oldest row must fall back to the text placeholder instead of loading.
	big := bytes.Repeat([]byte{0x89}, 5<<19) // 2.5 MB
	names := []string{"old-1.png", "old-2.png", "newest.png"}
	for _, name := range names[:2] {
		if err := os.WriteFile(filepath.Join(root, "images", name), big, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := s.InsertMessage("alice", "look ![图](/uploads/images/"+name+")", "", room, "", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "images", names[2]), big, 0644); err != nil {
		t.Fatal(err)
	}
	trigger, err := s.InsertMessage("alice", "@TokenBot ![图](/uploads/images/"+names[2]+")", "", room, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	h.enqueueRun(alice, trigger, false)
	call := nextCall(t, model)

	var attached, placeholders int
	for _, m := range call.input {
		attached += len(m.Images)
		if strings.Contains(m.Content, "[图片]") {
			placeholders++
		}
	}
	if attached != 2 {
		t.Fatalf("attached images = %d, want 2 within the total budget", attached)
	}
	if placeholders < 3 {
		t.Fatalf("image placeholders = %d, want every reference to keep its text marker", placeholders)
	}
	finishCall(call, "done")
}

func TestEnqueueRunVisionDisabledStaysTextOnly(t *testing.T) {
	h, s, model := runTestHub(t)
	root := t.TempDir()
	writeUploadPNG(t, root, testPNGName)
	// Existing default: vision off even though the upload exists.
	h.uploadsDir = root
	room := h.DefaultRoomID()
	alice := runClient(h, "alice", "account-A", room)
	trigger, err := s.InsertMessage("alice", "@TokenBot ![图]("+testPNGRef+")", "", room, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	h.enqueueRun(alice, trigger, false)
	call := nextCall(t, model)
	for i, m := range call.input {
		if len(m.Images) != 0 {
			t.Fatalf("row %d has %d images with vision disabled", i, len(m.Images))
		}
	}
	if !strings.Contains(call.input[len(call.input)-1].Content, "[图片]") {
		t.Errorf("trigger lost its image placeholder")
	}
	finishCall(call, "done")
}

func TestEnqueueRunMissingImageDoesNotBlock(t *testing.T) {
	h, s, model := runTestHub(t)
	h.visionEnabled = true
	h.uploadsDir = t.TempDir()
	room := h.DefaultRoomID()
	alice := runClient(h, "alice", "account-A", room)
	trigger, err := s.InsertMessage("alice", "@TokenBot ![图](/uploads/images/missing.png)", "", room, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	h.enqueueRun(alice, trigger, false)
	call := nextCall(t, model)
	if len(call.input[len(call.input)-1].Images) != 0 {
		t.Fatal("missing image was attached")
	}
	finishCall(call, "done")
	rows, _ := s.RecentBotRuns(room, alice.actorID(), 1)
	if len(rows) != 1 {
		t.Fatalf("runs = %d", len(rows))
	}
	awaitStatus(t, s, rows[0].ID, "completed")
}
