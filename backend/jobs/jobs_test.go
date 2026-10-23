package jobs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/riverqueue/river"
)

type fakeStore struct {
	purged     time.Time
	expired    time.Time
	fail       error
	referenced map[string]bool
}

func (f *fakeStore) PurgeGuestIdentities(before time.Time) (int64, error) {
	f.purged = before
	return 2, f.fail
}

func (f *fakeStore) ExpireAgentMemories(now time.Time) (int64, error) {
	f.expired = now
	return 1, nil
}

func (f *fakeStore) IsUploadReferenced(filename string) (bool, error) {
	return f.referenced[filename], nil
}

func (f *fakeStore) TotalMessages() int64 { return 10 }

func TestMaintenanceWorkerSweepsWithCutoffs(t *testing.T) {
	store := &fakeStore{}
	worker := &maintenanceWorker{store: store}
	if err := worker.Work(context.Background(), &river.Job[MaintenanceArgs]{}); err != nil {
		t.Fatal(err)
	}
	// Guest bindings expire after the 90-day TTL, memories at "now".
	if age := time.Since(store.purged); age < guestIdentityTTL-time.Minute || age > guestIdentityTTL+time.Minute {
		t.Fatalf("guest cutoff = %v ago, want about %v", age, guestIdentityTTL)
	}
	if store.expired.IsZero() {
		t.Fatal("memory sweep did not run")
	}
}

func TestMaintenanceWorkerReturnsStoreErrors(t *testing.T) {
	worker := &maintenanceWorker{store: &fakeStore{fail: errors.New("db down")}}
	if err := worker.Work(context.Background(), &river.Job[MaintenanceArgs]{}); err == nil {
		t.Fatal("store failure was swallowed; River cannot retry it")
	}
}

func TestMaintenanceSweepRemovesOnlyOldUnreferencedUploads(t *testing.T) {
	dir := t.TempDir()
	images := filepath.Join(dir, "images")
	if err := os.MkdirAll(images, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	write := func(name string, mod time.Time) {
		path := filepath.Join(images, name)
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	write("orphan.png", old)
	write("kept.jpg", old)
	write("fresh.png", time.Now())

	store := &fakeStore{referenced: map[string]bool{"kept.jpg": true}}
	worker := &maintenanceWorker{store: store, uploadsDir: dir}
	removed, err := worker.sweepUploads(context.Background(), time.Now().Add(-uploadTTL))
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(filepath.Join(images, "orphan.png")); !os.IsNotExist(err) {
		t.Fatal("orphan upload survived the sweep")
	}
	for _, name := range []string{"kept.jpg", "fresh.png"} {
		if _, err := os.Stat(filepath.Join(images, name)); err != nil {
			t.Fatalf("%s was removed: %v", name, err)
		}
	}
}
