package handler

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const (
	maxImageUploadSize = 5 << 20 // 5 MB
	maxImageDimension  = 8192
	maxImagePixelCount = 14_000_000 // stays below the 96 MiB production memory limit after decode
)

// UploadImage handles POST /api/upload/image. Images are decoded and
// re-encoded before storage so EXIF/metadata never reaches a public URL.
func (h *Handler) UploadImage(w http.ResponseWriter, r *http.Request) {
	requestID := requestIDFromContext(r.Context())
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "METHOD_NOT_ALLOWED", requestID)
		return
	}
	// Guests may share images too; the per-IP bucket is tighter and the size
	// cap smaller than for signed-in accounts.
	_, authenticated, _ := h.optionalSession(w, r)
	limit, maxSize := 8, int64(2<<20)
	if authenticated {
		limit, maxSize = 30, int64(maxImageUploadSize)
	}
	if !uploadAllowed(requestIP(r), limit) {
		writeJSONError(w, http.StatusTooManyRequests, "too many uploads, please wait", "RATE_LIMITED", requestID)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSize+maxImageMultipartOverhead)
	if err := r.ParseMultipartForm(maxSize); err != nil {
		writeJSONError(w, http.StatusBadRequest, "image too large", "FILE_TOO_LARGE", requestID)
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "missing file field", "MISSING_FILE", requestID)
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
		writeJSONError(w, http.StatusBadRequest, "unsupported image type (allowed: png, jpg)", "INVALID_FILE_TYPE", requestID)
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil || len(data) == 0 || int64(len(data)) > maxSize {
		writeJSONError(w, http.StatusBadRequest, "image too large", "FILE_TOO_LARGE", requestID)
		return
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") {
		writeJSONError(w, http.StatusBadRequest, "file is not a valid png or jpeg image", "INVALID_FILE_TYPE", requestID)
		return
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxImageDimension ||
		cfg.Height > maxImageDimension || cfg.Width*cfg.Height > maxImagePixelCount {
		writeJSONError(w, http.StatusBadRequest, "image dimensions are too large", "INVALID_DIMENSIONS", requestID)
		return
	}

	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "file is not a valid image", "INVALID_FILE_TYPE", requestID)
		return
	}

	var encoded bytes.Buffer
	var name, contentType string
	if format == "png" {
		if err := png.Encode(&encoded, decoded); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to process image", "SERVER_ERROR", requestID)
			return
		}
		name = uuid.NewString() + ".png"
		contentType = "image/png"
	} else {
		if err := jpeg.Encode(&encoded, decoded, &jpeg.Options{Quality: 85}); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to process image", "SERVER_ERROR", requestID)
			return
		}
		name = uuid.NewString() + ".jpg"
		contentType = "image/jpeg"
	}

	mediaKey := "images/" + name
	if err := h.mediaStore.Save(r.Context(), mediaKey, contentType, &encoded); err != nil {
		log.Printf("image upload: failed to save %s: %v", mediaKey, err)
		writeJSONError(w, http.StatusInternalServerError, "failed to save image", "SERVER_ERROR", requestID)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"url":      "/uploads/images/" + name,
		"filename": name,
	})
}

// ServeImage handles GET /uploads/images/{filename}.
func (h *Handler) ServeImage(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/uploads/images/")
	filename := filepath.Base(trimmed)
	if filename == "." || filename == "/" || filename == "" || strings.Contains(trimmed, "/") {
		http.NotFound(w, r)
		return
	}
	media, err := h.mediaStore.Open(r.Context(), "images/"+filename)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer media.Body.Close()

	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'")
	if media.ContentType != "" {
		w.Header().Set("Content-Type", media.ContentType)
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if _, err := io.Copy(w, media.Body); err != nil {
		log.Printf("failed to stream image %s: %v", filename, err)
	}
}
