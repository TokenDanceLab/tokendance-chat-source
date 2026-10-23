// Package jobs runs River workers inside the chat process. River is a Go
// library backed by the same PostgreSQL database as the chat store; it is not
// another service. Jobs must stay idempotent because River retries them.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"tokendancechat/backend/hub"
)

// Guest identities untouched for this long are removed by maintenance. The
// binding is only a convenience; losing it means a new guest handle.
const guestIdentityTTL = 90 * 24 * time.Hour

// Uploads older than this and no longer referenced by any message are
// abandoned attachments (send failed, tab closed) and are removed.
const uploadTTL = 24 * time.Hour

// MaintenanceArgs is the daily housekeeping job.
type MaintenanceArgs struct{}

func (MaintenanceArgs) Kind() string { return "maintenance" }

// maintenanceStore is the narrow subset the sweep needs; keeping it tiny lets
// the worker be tested without a full hub.Store double.
type maintenanceStore interface {
	PurgeGuestIdentities(before time.Time) (int64, error)
	ExpireAgentMemories(now time.Time) (int64, error)
	IsUploadReferenced(filename string) (bool, error)
	TotalMessages() int64
}

type maintenanceWorker struct {
	river.WorkerDefaults[MaintenanceArgs]
	store      maintenanceStore
	uploadsDir string
}

func (w *maintenanceWorker) Work(ctx context.Context, _ *river.Job[MaintenanceArgs]) error {
	// The three sweeps are independent: one slow or failing step must not
	// starve the others on a large database.
	var errs []error
	var guests, expired, orphans int64
	if n, err := w.store.PurgeGuestIdentities(time.Now().Add(-guestIdentityTTL)); err != nil {
		errs = append(errs, fmt.Errorf("purge guest identities: %w", err))
	} else {
		guests = n
	}
	if n, err := w.store.ExpireAgentMemories(time.Now()); err != nil {
		errs = append(errs, fmt.Errorf("expire memories: %w", err))
	} else {
		expired = n
	}
	if n, err := w.sweepUploads(ctx, time.Now().Add(-uploadTTL)); err != nil {
		errs = append(errs, fmt.Errorf("sweep uploads: %w", err))
	} else {
		orphans = n
	}
	if guests > 0 || expired > 0 || orphans > 0 {
		log.Printf("maintenance: purged_guests=%d expired_memories=%d orphan_uploads=%d", guests, expired, orphans)
	}
	return errors.Join(errs...)
}

// sweepUploads deletes image files older than the cutoff that no message
// references. Referenced files are message assets and stay forever.
func (w *maintenanceWorker) sweepUploads(ctx context.Context, cutoff time.Time) (int64, error) {
	if w.uploadsDir == "" {
		return 0, nil
	}
	// A fresh or half-migrated database with an old uploads volume must not be
	// mistaken for "every image is an orphan".
	if w.store.TotalMessages() == 0 {
		log.Printf("maintenance: skip upload sweep on empty message store")
		return 0, nil
	}
	dir := filepath.Join(w.uploadsDir, "images")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var removed int64
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		switch strings.ToLower(filepath.Ext(name)) {
		case ".png", ".jpg", ".jpeg":
		default:
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		referenced, err := w.store.IsUploadReferenced(name)
		if err != nil {
			return removed, err
		}
		if referenced {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			// One stuck file (lock, permissions, NFS) must not block the whole
			// sweep or make River retry the same directory forever.
			log.Printf("maintenance: skip upload %s: %v", name, err)
			continue
		}
		removed++
	}
	return removed, nil
}

// Client owns the embedded River client. Close stops the workers.
type Client struct {
	inner *river.Client[pgx.Tx]
}

// Migrate applies River's own schema. It needs a DDL-capable connection and is
// therefore called from the migration path (admin DSN), never by the runtime
// role.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("river migrator: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("river migrate: %w", err)
	}
	return nil
}

// Start begins the embedded workers against pool. The caller must Close it
// during shutdown.
func Start(ctx context.Context, pool *pgxpool.Pool, store hub.Store, uploadsDir string) (*Client, error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &maintenanceWorker{store: store, uploadsDir: uploadsDir})

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 1},
		},
		// A large uploads directory can take minutes; the default one-minute
		// timeout would cancel and restart the sweep from the top.
		JobTimeout:      10 * time.Minute,
		SoftStopTimeout: 20 * time.Second,
		Workers:         workers,
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(
				river.PeriodicInterval(24*time.Hour),
				func() (river.JobArgs, *river.InsertOpts) { return MaintenanceArgs{}, nil },
				&river.PeriodicJobOpts{ID: "chat-maintenance", RunOnStart: true},
			),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("river client: %w", err)
	}
	if err := client.Start(ctx); err != nil {
		return nil, fmt.Errorf("river start: %w", err)
	}
	return &Client{inner: client}, nil
}

// Close stops the workers gracefully.
func (c *Client) Close(ctx context.Context) error {
	if c == nil || c.inner == nil {
		return nil
	}
	return c.inner.Stop(ctx)
}
