package llm

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPublicPartsRebuildsTextWithoutMutatingInput(t *testing.T) {
	parts := []Part{
		{Type: "reasoning", Text: "secret thinking"},
		{Type: "text", Text: "first", ToolCallID: "secret-id", ToolName: "secret tool", Arguments: "secret input", Result: "secret result", Status: "secret status"},
		{Type: "tool-call", Arguments: "secret input", Result: "secret result"},
		{Type: "future-part", Text: "secret future payload"},
		{Type: "text", Text: "second"},
	}
	original := append([]Part(nil), parts...)
	got := PublicParts(parts)
	want := []Part{{Type: "text", Text: "first"}, {Type: "text", Text: "second"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("public parts = %#v, want %#v", got, want)
	}
	got[0].Text = "changed"
	if !reflect.DeepEqual(parts, original) {
		t.Fatalf("public projection mutated private/model input: %#v", parts)
	}
}

func TestPublicPartsJSONFailsClosed(t *testing.T) {
	tests := []struct {
		name, raw, want string
	}{
		{"known and unknown fields", `[{"type":"reasoning","text":"secret"},{"type":"tool-call","arguments":"secret","result":"secret"},{"type":"future-part","text":"secret"},{"type":"text","text":"answer","arguments":"secret","result":"secret","metadata":{"secret":true}}]`, `[{"type":"text","text":"answer"}]`},
		{"empty", "", ""},
		{"null", "null", ""},
		{"empty array", "[]", ""},
		{"no public text", `[{"type":"reasoning","text":"secret"}]`, ""},
		{"truncated", `[{"type":"text","text":"answer"},{"type":"reasoning","text":"secret"}`, ""},
		{"trailing value", `[{"type":"text","text":"answer"}] {"secret":true}`, ""},
		{"object", `{"type":"text","text":"answer","secret":true}`, ""},
		{"invalid text", `[{"type":"text","text":{"secret":true}}]`, ""},
		{"invalid array item", `[{"type":"text","text":"answer"}, "secret"]`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PublicPartsJSON(json.RawMessage(tt.raw)); string(got) != tt.want {
				t.Fatalf("public JSON = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPublicSourcesRebuildOnlySafeMetadata(t *testing.T) {
	parts := []Part{
		{Type: "source", SourceID: "registered", Title: "Official", URL: "https://example.com", Status: "fetched", Cited: true, Text: "secret", Arguments: "secret", Result: "secret", ToolName: "secret"},
		{Type: "source", SourceID: "candidate", URL: "https://example.org", Status: "discovered", Cited: true},
		{Type: "source", SourceID: "unsafe", URL: "javascript:alert(1)", Status: "fetched", Cited: true},
		{Type: "source", SourceID: "credentials", URL: "https://key:secret@example.com", Status: "fetched"}, // # leak-guard-allow:LG-F4C6A155 synthetic credentials rejected by this test
	}
	got := PublicParts(parts)
	want := []Part{{Type: "source", SourceID: "registered", Title: "Official", URL: "https://example.com", Status: "fetched", Cited: true}, {Type: "source", SourceID: "candidate", URL: "https://example.org", Status: "discovered"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("source projection: %#v", got)
	}
}
