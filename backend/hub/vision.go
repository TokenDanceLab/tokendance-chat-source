package hub

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"tokendancechat/backend/llm"
)

const (
	// A run may carry at most this many images. They are chosen from the
	// newest messages first, so the request that triggered the run always wins
	// over older history.
	maxRunImages = 3
	// Uploads are capped at 4 MB by the HTTP handler; this bound keeps a run's
	// outbound payload small even if a file was placed in uploads by hand.
	maxRunImageBytes = 4 << 20
	// The per-run total keeps a queue of runs from holding tens of megabytes of
	// base64 each; images beyond the budget stay as text placeholders.
	maxRunImageTotalBytes = 6 << 20

	uploadsImagesPrefix = "/uploads/images/"
)

var (
	// Only relative, local upload paths count. Absolute URLs and query strings
	// are intentionally unsupported: they would let a chat message make the
	// server fetch arbitrary remote content.
	imageRefPattern = regexp.MustCompile(`(?i)!\[[^\]]*\]\((/uploads/images/[A-Za-z0-9-]+\.(?:png|jpg|jpeg))\)`)

	errImageRef     = errors.New("unsupported image reference")
	errImageTooBig  = errors.New("image exceeds the vision size limit")
	errImageNotFile = errors.New("image is not a regular file")
)

// extractImageRefs replaces Markdown image references with a plain placeholder
// so the model is told an image is present without receiving a URL it cannot
// open. Every occurrence keeps its own placeholder; refs are deduplicated in
// first-seen order.
func extractImageRefs(content string) (string, []string) {
	matches := imageRefPattern.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return content, nil
	}
	var refs []string
	seen := make(map[string]bool, len(matches))
	var text strings.Builder
	last := 0
	for _, match := range matches {
		text.WriteString(content[last:match[0]])
		text.WriteString("[图片]")
		last = match[1]
		ref := content[match[2]:match[3]]
		if !seen[ref] {
			seen[ref] = true
			refs = append(refs, ref)
		}
	}
	text.WriteString(content[last:])
	return text.String(), refs
}

// loadImageDataURL reads one local /uploads/images reference and returns it as
// a data URL. The reference is cleaned and must resolve inside
// <uploadsDir>/images; symlinks and non-regular files are rejected because the
// bot runs with the server's filesystem privileges.
func loadImageDataURL(uploadsDir, ref string, limit int64) (string, int64, error) {
	if strings.TrimSpace(uploadsDir) == "" {
		return "", 0, errors.New("uploads directory is not configured")
	}
	cleaned := filepath.ToSlash(filepath.Clean(ref))
	if !strings.HasPrefix(cleaned, uploadsImagesPrefix) {
		return "", 0, errImageRef
	}
	name := strings.TrimPrefix(cleaned, uploadsImagesPrefix)
	if name == "" || name == "." || name == ".." || strings.Contains(name, "/") {
		return "", 0, errImageRef
	}
	var mime string
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		mime = "image/png"
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	default:
		return "", 0, errImageRef
	}

	root, err := filepath.Abs(filepath.Join(uploadsDir, "images"))
	if err != nil {
		return "", 0, err
	}
	path, err := filepath.Abs(filepath.Join(root, name))
	if err != nil {
		return "", 0, err
	}
	if filepath.Dir(path) != root {
		return "", 0, errImageRef
	}

	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() {
		return "", 0, errImageNotFile
	}
	if info.Size() <= 0 || info.Size() > limit {
		return "", 0, fmt.Errorf("%w (%d bytes)", errImageTooBig, info.Size())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	if int64(len(data)) == 0 || int64(len(data)) > limit {
		return "", 0, fmt.Errorf("%w (%d bytes)", errImageTooBig, len(data))
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), int64(len(data)), nil
}

// attachImages loads referenced uploads and attaches them to user messages.
// refsByMessage is index-aligned with input. Messages are visited newest
// first so the per-run budget goes to the trigger and recent context; once the
// budget is spent the remaining messages keep only their text placeholder.
// Failures are non-fatal: the run proceeds without that image.
func (h *Hub) attachImages(input []llm.Message, refsByMessage [][]string) {
	if !h.visionEnabled || h.uploadsDir == "" {
		return
	}
	seen := make(map[string]bool)
	loaded := 0
	var totalBytes int64
	for i := len(input) - 1; i >= 0; i-- {
		if input[i].Role != "user" {
			continue
		}
		for _, ref := range refsByMessage[i] {
			if seen[ref] {
				continue
			}
			seen[ref] = true
			if loaded >= maxRunImages || totalBytes >= maxRunImageTotalBytes {
				continue
			}
			limit := int64(maxRunImageBytes)
			if remaining := int64(maxRunImageTotalBytes) - totalBytes; remaining < limit {
				limit = remaining
			}
			dataURL, size, err := loadImageDataURL(h.uploadsDir, ref, limit)
			if err != nil {
				// Path and reason only — never the image payload.
				log.Printf("vision: skipping image %q: %v", filepath.Base(ref), err)
				continue
			}
			input[i].Images = append(input[i].Images, dataURL)
			totalBytes += size
			loaded++
		}
	}
}
