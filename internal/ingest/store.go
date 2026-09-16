package ingest

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"solar-collector/internal/model"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	// WAL mode lets the ingest loop's writes and the replayer's reads run
	// concurrently; busy_timeout makes a writer wait for a brief lock
	// instead of failing immediately with SQLITE_BUSY.
	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	schema := `
	CREATE TABLE IF NOT EXISTS readings (
		id TEXT PRIMARY KEY,
		source TEXT NOT NULL,
		tag TEXT NOT NULL,
		value REAL NOT NULL,
		timestamp TEXT NOT NULL,
		synced INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_readings_synced ON readings(synced);

	CREATE TABLE IF NOT EXISTS alarms (
		id TEXT PRIMARY KEY,
		tag TEXT NOT NULL,
		condition TEXT NOT NULL,
		value REAL NOT NULL,
		raised_at TEXT NOT NULL,
		cleared_at TEXT,
		synced INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_alarms_synced ON alarms(synced);

	CREATE TABLE IF NOT EXISTS rollups (
		tag TEXT NOT NULL,
		window_start TEXT NOT NULL,
		window_end TEXT NOT NULL,
		min_value REAL NOT NULL,
		max_value REAL NOT NULL,
		avg_value REAL NOT NULL,
		sample_count INTEGER NOT NULL,
		synced INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (tag, window_start)
	);
	CREATE INDEX IF NOT EXISTS idx_rollups_synced ON rollups(synced);
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// Put is idempotent: writing the same reading ID twice (e.g. after a
// restart mid-poll) leaves the row unchanged instead of duplicating it.
func (s *Store) Put(ctx context.Context, r model.Reading) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO readings (id, source, tag, value, timestamp, synced)
		VALUES (?, ?, ?, ?, ?, 0)
		ON CONFLICT(id) DO NOTHING
	`, r.ID, r.Source, r.Tag, r.Value, r.Timestamp.Format(timeLayout))
	return err
}

func (s *Store) Unsynced(ctx context.Context, limit int) ([]model.Reading, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, source, tag, value, timestamp FROM readings
		WHERE synced = 0 ORDER BY timestamp ASC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Reading
	for rows.Next() {
		var r model.Reading
		var ts string
		if err := rows.Scan(&r.ID, &r.Source, &r.Tag, &r.Value, &ts); err != nil {
			return nil, err
		}
		parsed, err := parseTime(ts)
		if err != nil {
			return nil, err
		}
		r.Timestamp = parsed
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) MarkSynced(ctx context.Context, ids []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `UPDATE readings SET synced = 1 WHERE id = ?`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, id := range ids {
		if _, err := stmt.ExecContext(ctx, id); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Recent(ctx context.Context, tag string, since string) ([]model.Reading, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, source, tag, value, timestamp FROM readings
		WHERE tag = ? AND timestamp >= ? ORDER BY timestamp ASC
	`, tag, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Reading
	for rows.Next() {
		var r model.Reading
		var ts string
		if err := rows.Scan(&r.ID, &r.Source, &r.Tag, &r.Value, &ts); err != nil {
			return nil, err
		}
		parsed, err := parseTime(ts)
		if err != nil {
			return nil, err
		}
		r.Timestamp = parsed
		out = append(out, r)
	}
	return out, rows.Err()
}

// PutAlarm upserts by alarm ID. A raise inserts a fresh row with
// cleared_at NULL; a later clear for the same ID (same alarm) updates
// cleared_at on that row and resets synced to 0, so the change gets
// replayed even if the raise had already synced.
func (s *Store) PutAlarm(ctx context.Context, a model.Alarm) error {
	var clearedAt sql.NullString
	if a.ClearedAt != nil {
		clearedAt = sql.NullString{String: a.ClearedAt.Format(timeLayout), Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO alarms (id, tag, condition, value, raised_at, cleared_at, synced)
		VALUES (?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(id) DO UPDATE SET cleared_at = excluded.cleared_at, synced = 0
	`, a.ID, a.Tag, a.Condition, a.Value, a.RaisedAt.Format(timeLayout), clearedAt)
	return err
}

func (s *Store) UnsyncedAlarms(ctx context.Context, limit int) ([]model.Alarm, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tag, condition, value, raised_at, cleared_at FROM alarms
		WHERE synced = 0 ORDER BY raised_at ASC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Alarm
	for rows.Next() {
		var a model.Alarm
		var raisedAt string
		var clearedAt sql.NullString
		if err := rows.Scan(&a.ID, &a.Tag, &a.Condition, &a.Value, &raisedAt, &clearedAt); err != nil {
			return nil, err
		}
		parsed, err := parseTime(raisedAt)
		if err != nil {
			return nil, err
		}
		a.RaisedAt = parsed
		if clearedAt.Valid {
			ct, err := parseTime(clearedAt.String)
			if err != nil {
				return nil, err
			}
			a.ClearedAt = &ct
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) MarkAlarmsSynced(ctx context.Context, ids []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `UPDATE alarms SET synced = 1 WHERE id = ?`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, id := range ids {
		if _, err := stmt.ExecContext(ctx, id); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// PutRollup inserts a completed rollup window. Rollups are immutable once
// computed, so a retry after a crash just no-ops instead of erroring.
func (s *Store) PutRollup(ctx context.Context, r model.Rollup) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO rollups (tag, window_start, window_end, min_value, max_value, avg_value, sample_count, synced)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(tag, window_start) DO NOTHING
	`, r.Tag, r.WindowStart.Format(timeLayout), r.WindowEnd.Format(timeLayout), r.Min, r.Max, r.Avg, r.Count)
	return err
}

func (s *Store) UnsyncedRollups(ctx context.Context, limit int) ([]model.Rollup, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT tag, window_start, window_end, min_value, max_value, avg_value, sample_count FROM rollups
		WHERE synced = 0 ORDER BY window_start ASC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Rollup
	for rows.Next() {
		var r model.Rollup
		var windowStart, windowEnd string
		if err := rows.Scan(&r.Tag, &windowStart, &windowEnd, &r.Min, &r.Max, &r.Avg, &r.Count); err != nil {
			return nil, err
		}
		ws, err := parseTime(windowStart)
		if err != nil {
			return nil, err
		}
		we, err := parseTime(windowEnd)
		if err != nil {
			return nil, err
		}
		r.WindowStart = ws
		r.WindowEnd = we
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkRollupsSynced matches on (tag, window_start) rather than a
// synthetic string ID, since rollups have no natural single-column ID —
// the replayer already has the full model.Rollup values in hand.
func (s *Store) MarkRollupsSynced(ctx context.Context, rollups []model.Rollup) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `UPDATE rollups SET synced = 1 WHERE tag = ? AND window_start = ?`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, r := range rollups {
		if _, err := stmt.ExecContext(ctx, r.Tag, r.WindowStart.Format(timeLayout)); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
