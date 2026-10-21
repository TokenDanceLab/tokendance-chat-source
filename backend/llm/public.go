package llm

import (
	"encoding/json"
	"tokendancechat/backend/websearch"
)

// PublicParts projects an assistant reply onto the public conversation
// contract: text and server-registered source metadata are visible. Rebuild
// each part so tool fields cannot cross the boundary. Registration is checked
// by the store's final delivery transaction. The input stays intact for
// the model's tool loop and private conversations.
func PublicParts(parts []Part) []Part {
	var public []Part
	for _, part := range parts {
		if part.Type == "text" {
			public = append(public, Part{Type: "text", Text: part.Text})
		}
		if part.Type == "source" && part.SourceID != "" && websearch.SafeURL(part.URL) && (part.Status == "discovered" || part.Status == "fetched" || part.Status == "failed") {
			public = append(public, Part{Type: "source", SourceID: part.SourceID, Title: websearch.Clip(part.Title, 200), URL: part.URL, Status: part.Status, Cited: part.Cited && part.Status == "fetched"})
		}
	}
	return public
}

// PublicPartsJSON applies the same public boundary to persisted JSON,
// including rows written before that boundary existed. Invalid data produces
// no parts so clients can fall back to message content; raw JSON is never
// passed through. This does not rewrite the original stored row.
func PublicPartsJSON(raw json.RawMessage) json.RawMessage {
	var parts []Part
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil
	}
	public := PublicParts(parts)
	if len(public) == 0 {
		return nil
	}
	data, err := json.Marshal(public)
	if err != nil {
		return nil
	}
	return data
}

// WithoutSourcesJSON prevents legacy/non-run message paths from publishing
// evidence metadata. Only CompleteBotRun can register sources for a message.
func WithoutSourcesJSON(raw json.RawMessage) json.RawMessage {
	var parts []Part
	if json.Unmarshal(raw, &parts) != nil {
		return nil
	}
	filtered := parts[:0]
	for _, part := range parts {
		if part.Type != "source" {
			filtered = append(filtered, part)
		}
	}
	if len(filtered) == len(parts) {
		return raw
	}
	if len(filtered) == 0 {
		return nil
	}
	data, _ := json.Marshal(filtered)
	return data
}
