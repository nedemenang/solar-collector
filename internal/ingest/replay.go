package ingest

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"solar-collector/internal/model"
)

type Sink interface {
	Write(ctx context.Context, readings []model.Reading) error
	WriteAlarms(ctx context.Context, alarms []model.Alarm) error
	WriteRollups(ctx context.Context, rollups []model.Rollup) error
}

// bufferStore is the subset of *Store that Replayer depends on. Extracted
// as an interface so tests can inject a fake instead of a real SQLite
// database; *Store satisfies this structurally, with no changes needed at
// its call sites.
type bufferStore interface {
	Unsynced(ctx context.Context, limit int) ([]model.Reading, error)
	MarkSynced(ctx context.Context, ids []string) error
	UnsyncedAlarms(ctx context.Context, limit int) ([]model.Alarm, error)
	MarkAlarmsSynced(ctx context.Context, ids []string) error
	UnsyncedRollups(ctx context.Context, limit int) ([]model.Rollup, error)
	MarkRollupsSynced(ctx context.Context, rollups []model.Rollup) error
}

// Replayer periodically drains unsynced readings from the local buffer to
// the cloud sink. When the sink is unreachable (e.g. the link is cut) it
// simply retries on the next tick — readings keep accumulating locally in
// the meantime, so nothing is lost, and nothing is double-sent because rows
// are only marked synced after a successful write.
type Replayer struct {
	store    bufferStore
	sink     Sink
	interval time.Duration
	batch    int
}

func NewReplayer(store *Store, sink Sink, interval time.Duration) *Replayer {
	return &Replayer{store: store, sink: sink, interval: interval, batch: 200}
}

func (r *Replayer) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.tick(ctx); err != nil {
				slog.Warn("replay tick failed, will retry", "error", err)
			}
		}
	}
}

// tick drains all three local tables unconditionally each interval — one
// resource failing to replay must not block the other two from draining
// in the same tick.
func (r *Replayer) tick(ctx context.Context) error {
	return errors.Join(
		r.tickReadings(ctx),
		r.tickAlarms(ctx),
		r.tickRollups(ctx),
	)
}

func (r *Replayer) tickReadings(ctx context.Context) error {
	pending, err := r.store.Unsynced(ctx, r.batch)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}

	if err := r.sink.Write(ctx, pending); err != nil {
		return err
	}

	ids := make([]string, len(pending))
	for i, p := range pending {
		ids[i] = p.ID
	}
	if err := r.store.MarkSynced(ctx, ids); err != nil {
		return err
	}

	slog.Info("replayed readings to sink", "count", len(pending))
	return nil
}

func (r *Replayer) tickAlarms(ctx context.Context) error {
	pending, err := r.store.UnsyncedAlarms(ctx, r.batch)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}

	if err := r.sink.WriteAlarms(ctx, pending); err != nil {
		return err
	}

	ids := make([]string, len(pending))
	for i, p := range pending {
		ids[i] = p.ID
	}
	if err := r.store.MarkAlarmsSynced(ctx, ids); err != nil {
		return err
	}

	slog.Info("replayed alarms to sink", "count", len(pending))
	return nil
}

func (r *Replayer) tickRollups(ctx context.Context) error {
	pending, err := r.store.UnsyncedRollups(ctx, r.batch)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}

	if err := r.sink.WriteRollups(ctx, pending); err != nil {
		return err
	}

	if err := r.store.MarkRollupsSynced(ctx, pending); err != nil {
		return err
	}

	slog.Info("replayed rollups to sink", "count", len(pending))
	return nil
}
