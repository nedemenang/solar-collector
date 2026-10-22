package sink

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"solar-collector/internal/model"
)

// pgxBatcher is the subset of *pgxpool.Pool that Postgres depends on. It
// exists so tests can substitute a fake batcher instead of dialing a real
// Postgres instance. *pgxpool.Pool satisfies this interface structurally.
type pgxBatcher interface {
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
	Close()
}

type Postgres struct {
	pool pgxBatcher
}

// NewPostgres only parses the DSN and builds a connection pool — it does
// not dial. The sink may not be reachable yet (Toxiproxy's proxy might not
// exist, or the link might be cut), and that's fine: the pool dials lazily
// per query, so Write simply fails and the caller (the replayer) retries
// on its own schedule until the sink comes back.
func NewPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close() {
	p.pool.Close()
}

// Write upserts by (reading ID, ts), so replaying the same batch twice
// (e.g. the process crashes after writing but before marking rows synced
// locally) never produces duplicate rows in the sink. The conflict target
// includes ts, not just id, because readings is a TimescaleDB hypertable
// partitioned on ts (see db/migrations/0002_readings_hypertable.sql) —
// every unique index on a hypertable must include the partitioning
// column. This doesn't weaken idempotency: a given reading ID is only
// ever produced once, with one fixed ts, so (id, ts) identifies the same
// row as id alone would.
func (p *Postgres) Write(ctx context.Context, readings []model.Reading) error {
	batch := &pgx.Batch{}
	for _, r := range readings {
		batch.Queue(`
			INSERT INTO readings (id, source, tag, value, ts)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (id, ts) DO UPDATE SET value = EXCLUDED.value
		`, r.ID, r.Source, r.Tag, r.Value, r.Timestamp)
	}

	br := p.pool.SendBatch(ctx, batch)
	defer br.Close()

	for range readings {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return br.Close()
}

// WriteAlarms upserts by alarm ID: a raise inserts the row, a later clear
// for the same ID updates cleared_at on it, so replaying either event
// twice never produces duplicate or inconsistent rows.
func (p *Postgres) WriteAlarms(ctx context.Context, alarms []model.Alarm) error {
	batch := &pgx.Batch{}
	for _, a := range alarms {
		batch.Queue(`
			INSERT INTO alarms (id, tag, condition, value, raised_at, cleared_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (id) DO UPDATE SET cleared_at = EXCLUDED.cleared_at
		`, a.ID, a.Tag, a.Condition, a.Value, a.RaisedAt, a.ClearedAt)
	}

	br := p.pool.SendBatch(ctx, batch)
	defer br.Close()

	for range alarms {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return br.Close()
}

// WriteRollups inserts completed rollup windows. Rollups are immutable
// once computed, so a retry after a crash just no-ops on the duplicate
// (tag, window_start) instead of erroring.
func (p *Postgres) WriteRollups(ctx context.Context, rollups []model.Rollup) error {
	batch := &pgx.Batch{}
	for _, r := range rollups {
		batch.Queue(`
			INSERT INTO rollups (tag, window_start, window_end, min_value, max_value, avg_value, sample_count)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (tag, window_start) DO NOTHING
		`, r.Tag, r.WindowStart, r.WindowEnd, r.Min, r.Max, r.Avg, r.Count)
	}

	br := p.pool.SendBatch(ctx, batch)
	defer br.Close()

	for range rollups {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return br.Close()
}
