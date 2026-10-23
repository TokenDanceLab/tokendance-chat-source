package hub

import (
	"strings"
)

// finalSplitMarker is the explicit token the model places on its own line to
// split one final answer into several short chat messages, the way a person
// sends a thought as a small burst instead of one paragraph. The marker must
// occupy a whole line so prose can never trigger a split by accident.
const finalSplitMarker = "<msg-break>"

// finalSplitMax bounds how many messages one run may publish as its final
// answer: a room stays tighter than a 1:1 conversation.
func finalSplitMax(mode string) int {
	if mode == "private" {
		return 4
	}
	return 3
}

// splitFinalMessage splits the final reply on whole-line markers. Markers
// inside fenced code blocks are literal content, not split points; empty
// segments are dropped; when the burst exceeds the mode limit the tail is
// merged back into the last message instead of being truncated away.
func splitFinalMessage(text string, mode string) []string {
	if !strings.Contains(text, finalSplitMarker) {
		return nil
	}
	var segments []string
	var current strings.Builder
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
		}
		if !inFence && trimmed == finalSplitMarker {
			if seg := sanitizeBotContent(current.String()); seg != "" {
				segments = append(segments, seg)
			}
			current.Reset()
			continue
		}
		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(line)
	}
	if seg := sanitizeBotContent(current.String()); seg != "" {
		segments = append(segments, seg)
	}
	if len(segments) < 2 {
		return nil
	}
	if max := finalSplitMax(mode); len(segments) > max {
		head := segments[:max-1]
		tail := strings.Join(segments[max-1:], "\n\n")
		segments = append(head, tail)
	}
	return segments
}
