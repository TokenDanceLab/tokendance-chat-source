package handler

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func multipartImage(t *testing.T, name string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return body, writer.FormDataContentType()
}

func samplePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func noisyPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	seed := uint32(1)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			seed = seed*1664525 + 1013904223
			img.Set(x, y, color.RGBA{R: uint8(seed), G: uint8(seed >> 8), B: uint8(seed >> 16), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode noisy png: %v", err)
	}
	return buf.Bytes()
}

func pngHeader(t *testing.T, width, height uint32) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], width)
	binary.BigEndian.PutUint32(ihdr[4:8], height)
	ihdr[8] = 8
	ihdr[9] = 6
	if err := binary.Write(&buf, binary.BigEndian, uint32(len(ihdr))); err != nil {
		t.Fatalf("write ihdr length: %v", err)
	}
	buf.WriteString("IHDR")
	buf.Write(ihdr)
	if err := binary.Write(&buf, binary.BigEndian, crc32.ChecksumIEEE(append([]byte("IHDR"), ihdr...))); err != nil {
		t.Fatalf("write ihdr crc: %v", err)
	}
	return buf.Bytes()
}

func TestUploadImageReEncodesAndServes(t *testing.T) {
	dir := t.TempDir()
	h := newTestHandler()
	h.uploadsDir = dir
	h.mediaStore = NewLocalMediaStore(dir)

	body, contentType := multipartImage(t, "photo.png", samplePNG(t))
	req := httptest.NewRequest(http.MethodPost, "/api/upload/image", body)
	req.Header.Set("Content-Type", contentType)
	authorizeTestRequest(t, h, req, "alice")
	w := httptest.NewRecorder()
	h.UploadImage(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, w.Body.String())
	}
	var payload map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.HasPrefix(payload["url"], "/uploads/images/") {
		t.Fatalf("unexpected url %q", payload["url"])
	}
	if !strings.HasSuffix(payload["filename"], ".png") {
		t.Fatalf("unexpected filename %q", payload["filename"])
	}

	media, err := h.mediaStore.Open(context.Background(), "images/"+payload["filename"])
	if err != nil {
		t.Fatalf("open stored image: %v", err)
	}
	defer media.Body.Close()
	stored, err := io.ReadAll(media.Body)
	if err != nil {
		t.Fatalf("read stored image: %v", err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(stored))
	if err != nil || format != "png" || cfg.Width != 3 || cfg.Height != 2 {
		t.Fatalf("stored image did not round-trip: format=%q cfg=%+v err=%v", format, cfg, err)
	}

	serveReq := httptest.NewRequest(http.MethodGet, payload["url"], nil)
	serveW := httptest.NewRecorder()
	h.ServeImage(serveW, serveReq)
	serveResp := serveW.Result()
	defer serveResp.Body.Close()
	if serveResp.StatusCode != http.StatusOK {
		t.Fatalf("serve status = %d", serveResp.StatusCode)
	}
	if got := serveResp.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("content type = %q", got)
	}
	if got := serveResp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("nosniff missing: %q", got)
	}
	if got := serveResp.Header.Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("cache control = %q", got)
	}
}

func TestUploadImagePassesGlobalBodyLimit(t *testing.T) {
	dir := t.TempDir()
	h := newTestHandler()
	h.uploadsDir = dir
	h.mediaStore = NewLocalMediaStore(dir)

	data := noisyPNG(t, 160, 160)
	if len(data) <= maxJSONBodySize {
		t.Fatalf("test image must exceed the JSON body limit, got %d bytes", len(data))
	}
	body, contentType := multipartImage(t, "photo.png", data)
	req := httptest.NewRequest(http.MethodPost, "/api/upload/image", body)
	req.Header.Set("Content-Type", contentType)
	authorizeTestRequest(t, h, req, "alice")
	wrapped := BodyLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.UploadImage(w, r)
	}))
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("image upload status = %d: %s", w.Result().StatusCode, w.Body.String())
	}
}

func TestUploadImageRejectsOversizedPixelDimensionsBeforeDecode(t *testing.T) {
	dir := t.TempDir()
	h := newTestHandler()
	h.uploadsDir = dir
	h.mediaStore = NewLocalMediaStore(dir)

	body, contentType := multipartImage(t, "huge.png", pngHeader(t, 2001, 2000))
	req := httptest.NewRequest(http.MethodPost, "/api/upload/image", body)
	req.Header.Set("Content-Type", contentType)
	authorizeTestRequest(t, h, req, "alice")
	wrapped := BodyLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.UploadImage(w, r)
	}))
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("oversized image status = %d: %s", w.Result().StatusCode, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "INVALID_DIMENSIONS") {
		t.Fatalf("oversized image response = %s", w.Body.String())
	}
}

func TestUploadImageRejectsNonImageAndTraversal(t *testing.T) {
	dir := t.TempDir()
	h := newTestHandler()
	h.uploadsDir = dir
	h.mediaStore = NewLocalMediaStore(dir)

	body, contentType := multipartImage(t, "notes.png", []byte("not an image"))
	req := httptest.NewRequest(http.MethodPost, "/api/upload/image", body)
	req.Header.Set("Content-Type", contentType)
	authorizeTestRequest(t, h, req, "alice")
	w := httptest.NewRecorder()
	h.UploadImage(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("non-image status = %d", w.Result().StatusCode)
	}

	traversal := httptest.NewRequest(http.MethodGet, "/uploads/images/../emojis/x.png", nil)
	tw := httptest.NewRecorder()
	h.ServeImage(tw, traversal)
	if tw.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("traversal status = %d", tw.Result().StatusCode)
	}
}

func TestUploadImageBoundsConcurrentProcessingBeforeReadingBody(t *testing.T) {
	ResetRateLimiter()
	imageUploadSlot <- struct{}{}
	defer func() { <-imageUploadSlot }()
	h := newTestHandler()
	body, contentType := multipartImage(t, "photo.png", samplePNG(t))
	req := httptest.NewRequest(http.MethodPost, "/api/upload/image", body)
	req.Header.Set("Content-Type", contentType)
	req.RemoteAddr = "192.0.2.90:1234"
	w := httptest.NewRecorder()
	h.UploadImage(w, req)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "1" {
		t.Fatalf("busy image processing should return retryable 429, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUploadImageAllowsGuestsWithinRateLimit(t *testing.T) {
	ResetRateLimiter()
	dir := t.TempDir()
	h := newTestHandler()
	h.uploadsDir = dir
	h.mediaStore = NewLocalMediaStore(dir)
	body, contentType := multipartImage(t, "photo.png", samplePNG(t))
	req := httptest.NewRequest(http.MethodPost, "/api/upload/image", body)
	req.Header.Set("Content-Type", contentType)
	req.RemoteAddr = "192.0.2.55:1234"
	w := httptest.NewRecorder()
	h.UploadImage(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("guest upload status = %d: %s", w.Result().StatusCode, w.Body.String())
	}
}

func TestUploadImageRateLimitsGuests(t *testing.T) {
	ResetRateLimiter()
	dir := t.TempDir()
	h := newTestHandler()
	h.uploadsDir = dir
	h.mediaStore = NewLocalMediaStore(dir)
	var last int
	for i := 0; i < 9; i++ {
		body, contentType := multipartImage(t, "photo.png", samplePNG(t))
		req := httptest.NewRequest(http.MethodPost, "/api/upload/image", body)
		req.Header.Set("Content-Type", contentType)
		req.RemoteAddr = "192.0.2.77:1234"
		w := httptest.NewRecorder()
		h.UploadImage(w, req)
		last = w.Result().StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("guest should be rate limited, last status = %d", last)
	}
}
