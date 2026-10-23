package storetest_test

import (
	"path/filepath"
	"testing"

	"tokendancechat/backend/internal/storetest"
	"tokendancechat/backend/store"
)

var _ storetest.Store = (*store.Store)(nil)

// TestSQLiteContract runs the shared suite against the incumbent implementation.
// A fresh database per case keeps the SQLite and PostgreSQL runs comparable.
func TestSQLiteContract(t *testing.T) {
	t.Setenv("CHAT_SKIP_SEED", "true")
	storetest.Run(t, func(t *testing.T) storetest.Store {
		s, err := store.New(filepath.Join(t.TempDir(), "chat.db"))
		if err != nil {
			t.Fatalf("store.New: %v", err)
		}
		t.Cleanup(func() {
			if err := s.Close(); err != nil {
				t.Errorf("close store: %v", err)
			}
		})
		return s
	})
}
