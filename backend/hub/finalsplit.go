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

// splitFinalMessage splits the final reply on the marker wherever it appears
// outside fenced code blocks. Models often place it between sentences on the
// same line rather than on a line of its own; both positions are split
// points. Empty segments are dropped; when the burst exceeds the mode limit
// the tail is merged back into the last message instead of being truncated.
func splitFinalMessage(text string, mode string) []string {
	if !strings.Contains(text, finalSplitMarker) {
		return nil
	}
	var segments []string
	var current strings.Builder
	inFence := false
	flush := func() {
		if seg := sanitizeBotContent(current.String()); seg != "" {
			segments = append(segments, seg)
		}
		current.Reset()
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
		}
		if !inFence && strings.Contains(line, finalSplitMarker) {
			// One line may carry several markers; the text before each marker
			// closes the current segment, the text after opens the next.
			for j, part := range strings.Split(line, finalSplitMarker) {
				if j > 0 {
					flush()
				}
				if part != "" {
					if current.Len() > 0 {
						current.WriteByte('\n')
					}
					current.WriteString(part)
				}
			}
			continue
		}
		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(line)
	}
	flush()
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
