package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const hiddenParts = `[{"type":"reasoning","text":"secretthinking"},{"type":"tool-call","arguments":"secretinput","result":"secretresult"},{"type":"text","text":"answer","arguments":"secretinput","result":"secretresult","metadata":{"secret":true}}]`
const visibleParts = `[{"type":"text","text":"answer"}]`

func TestPublicPartsNewWritesAndLegacyReads(t *testing.T) {
	for _, roomID := range []string{"", "room-1"} {
		t.Run("room="+roomID, func(t *testing.T) {
			t.Setenv("CHAT_SKIP_SEED", "true")
			s, err := New(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			parent, err := s.InsertMessage("alice", "question", "", roomID, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			msg, err := s.InsertMessageWithParts("TokenBot", "answer", roomID, "", parent.ID, json.RawMessage(hiddenParts))
			if err != nil {
				t.Fatal(err)
			}
			if string(msg.Parts) != visibleParts {
				t.Fatalf("insert returned hidden parts: %s", msg.Parts)
			}
			var raw string
			if err := s.db.QueryRow("SELECT parts FROM messages WHERE id = ?", msg.ID).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			if raw != visibleParts {
				t.Fatalf("new public row stored hidden data: %s", raw)
			}

			// Emulate a row written by the previous release, without filtering.
			if _, err := s.db.Exec("UPDATE messages SET parts = ? WHERE id = ?", hiddenParts, msg.ID); err != nil {
				t.Fatal(err)
			}
			if err := s.PinMessage(roomID, msg.ID, "alice"); err != nil {
				t.Fatal(err)
			}
			reads := map[string]func() ([]StoredMessage, error){
				"by ID":         func() ([]StoredMessage, error) { m, err := s.GetMessageByID(msg.ID); return []StoredMessage{m}, err },
				"history":       func() ([]StoredMessage, error) { return s.GetMessages(10, 0), nil },
				"room history":  func() ([]StoredMessage, error) { return s.GetRoomMessages(roomID, 10, 0), nil },
				"older history": func() ([]StoredMessage, error) { return s.GetRoomMessages(roomID, 10, msg.Timestamp+1), nil },
				"pins":          func() ([]StoredMessage, error) { return s.GetPinnedMessages(roomID), nil },
				"thread":        func() ([]StoredMessage, error) { return s.GetThreadMessages(parent.ID), nil },
				"export":        func() ([]StoredMessage, error) { return s.ExportMessages(context.Background(), roomID, "", "", "", 10) },
				"edit response": func() ([]StoredMessage, error) {
					m, err := s.UpdateMessage(msg.ID, "answer")
					return []StoredMessage{m}, err
				},
			}
			for name, read := range reads {
				t.Run(name, func(t *testing.T) {
					messages, err := read()
					if err != nil {
						t.Fatal(err)
					}
					found := false
					for _, got := range messages {
						if got.ID == msg.ID {
							found = true
							if string(got.Parts) != visibleParts || got.Content != "answer" {
								t.Fatalf("legacy public output = %+v; parts = %s", got, got.Parts)
							}
						}
					}
					if !found {
						t.Fatal("legacy public row not found")
					}
				})
			}
			results, err := s.SearchMessages("answer", roomID, 10)
			if err != nil || len(results) != 1 {
				t.Fatalf("visible search results = %+v, error = %v", results, err)
			}
			data, err := json.Marshal(results)
			if err != nil || strings.Contains(string(data), "secret") {
				t.Fatalf("search exposed hidden parts: %s, error = %v", data, err)
			}
			for _, query := range []string{"secretthinking", "secretinput", "secretresult"} {
				results, err := s.SearchMessages(query, roomID, 10)
				if err != nil || len(results) != 0 {
					t.Fatalf("search indexed %q: %+v, error = %v", query, results, err)
				}
			}
			if err := s.db.QueryRow("SELECT parts FROM messages WHERE id = ?", msg.ID).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			if raw != hiddenParts {
				t.Fatalf("legacy original was rewritten: %s", raw)
			}
		})
	}
}

func TestPublicMalformedPartsFailClosed(t *testing.T) {
	t.Setenv("CHAT_SKIP_SEED", "true")
	s, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	malformed := `[{"type":"text","text":"answer"},{"type":"reasoning","text":"secret"}`
	msg, err := s.InsertMessageWithParts("TokenBot", "answer", "", "", "", json.RawMessage(malformed))
	if err != nil || len(msg.Parts) != 0 {
		t.Fatalf("malformed insert = %+v, error = %v", msg, err)
	}
	var raw string
	if err := s.db.QueryRow("SELECT parts FROM messages WHERE id = ?", msg.ID).Scan(&raw); err != nil || raw != "" {
		t.Fatalf("malformed parts were persisted: %q, error = %v", raw, err)
	}
	if _, err := s.db.Exec("UPDATE messages SET parts = ? WHERE id = ?", malformed, msg.ID); err != nil {
		t.Fatal(err)
	}
	legacy, err := s.GetMessageByID(msg.ID)
	if err != nil || len(legacy.Parts) != 0 || legacy.Content != "answer" {
		t.Fatalf("malformed legacy output = %+v, error = %v", legacy, err)
	}
	data, err := json.Marshal(legacy)
	if err != nil || strings.Contains(string(data), "secret") {
		t.Fatalf("legacy fallback must be valid public JSON: %s, error = %v", data, err)
	}
}

func TestPrivatePartsRemainUnchanged(t *testing.T) {
	t.Setenv("CHAT_SKIP_SEED", "true")
	s, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	msg, err := s.InsertMessageWithParts("TokenBot", "answer", "", "alice", "", json.RawMessage(hiddenParts))
	if err != nil || string(msg.Parts) != hiddenParts {
		t.Fatalf("private insert = %+v, error = %v", msg, err)
	}
	var raw string
	if err := s.db.QueryRow("SELECT parts FROM messages WHERE id = ?", msg.ID).Scan(&raw); err != nil || raw != hiddenParts {
		t.Fatalf("private persisted parts = %q, error = %v", raw, err)
	}
	byID, err := s.GetMessageByID(msg.ID)
	if err != nil || string(byID.Parts) != hiddenParts || byID.ToUser != "alice" {
		t.Fatalf("private by ID = %+v, error = %v", byID, err)
	}
	history := s.GetMessagesBetween("alice", "TokenBot", 10)
	if len(history) != 1 || string(history[0].Parts) != hiddenParts {
		t.Fatalf("private history = %+v", history)
	}
	exported, err := s.ExportMessages(context.Background(), "", "TokenBot", "", "alice", 10)
	if err != nil || len(exported) != 1 || string(exported[0].Parts) != hiddenParts {
		t.Fatalf("private export = %+v, error = %v", exported, err)
	}
	if got := s.GetMessages(10, 0); len(got) != 0 {
		t.Fatalf("private row appeared in public history: %+v", got)
	}
	if got := s.GetMessagesBetween("bob", "TokenBot", 10); len(got) != 0 {
		t.Fatalf("private row appeared in another user's history: %+v", got)
	}
}
