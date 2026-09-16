package ingest

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"solar-collector/internal/model"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return store
}

func TestStorePutIdempotent(t *testing.T) {
	tests := []struct {
		name    string
		reading model.Reading
		times   int
	}{
		{
			name: "putting the same reading twice yields one row",
			reading: model.Reading{
				ID:        "x",
				Source:    "plc1",
				Tag:       "temp",
				Value:     42.5,
				Timestamp: time.Now().UTC(),
			},
			times: 2,
		},
		{
			name: "putting the same reading three times yields one row",
			reading: model.Reading{
				ID:        "y",
				Source:    "plc2",
				Tag:       "pressure",
				Value:     10.0,
				Timestamp: time.Now().UTC(),
			},
			times: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openTestStore(t)
			ctx := context.Background()

			for i := 0; i < tt.times; i++ {
				if err := store.Put(ctx, tt.reading); err != nil {
					t.Fatalf("Put() call %d error = %v", i, err)
				}
			}

			got, err := store.Unsynced(ctx, 100)
			if err != nil {
				t.Fatalf("Unsynced() error = %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("Unsynced() returned %d rows, want 1: %#v", len(got), got)
			}
			if got[0].ID != tt.reading.ID {
				t.Errorf("Unsynced()[0].ID = %q, want %q", got[0].ID, tt.reading.ID)
			}
		})
	}
}

func TestStoreUnsynced(t *testing.T) {
	base := time.Now().UTC()

	tests := []struct {
		name       string
		readings   []model.Reading // inserted in this order
		limit      int
		wantIDs    []string // expected IDs in returned order
	}{
		{
			name: "orders by timestamp ascending regardless of insertion order",
			readings: []model.Reading{
				{ID: "third", Source: "s", Tag: "t", Value: 3, Timestamp: base.Add(3 * time.Second)},
				{ID: "first", Source: "s", Tag: "t", Value: 1, Timestamp: base.Add(1 * time.Second)},
				{ID: "second", Source: "s", Tag: "t", Value: 2, Timestamp: base.Add(2 * time.Second)},
			},
			limit:   100,
			wantIDs: []string{"first", "second", "third"},
		},
		{
			name: "respects limit",
			readings: []model.Reading{
				{ID: "a", Source: "s", Tag: "t", Value: 1, Timestamp: base.Add(1 * time.Second)},
				{ID: "b", Source: "s", Tag: "t", Value: 2, Timestamp: base.Add(2 * time.Second)},
				{ID: "c", Source: "s", Tag: "t", Value: 3, Timestamp: base.Add(3 * time.Second)},
			},
			limit:   2,
			wantIDs: []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openTestStore(t)
			ctx := context.Background()

			for _, r := range tt.readings {
				if err := store.Put(ctx, r); err != nil {
					t.Fatalf("Put(%q) error = %v", r.ID, err)
				}
			}

			got, err := store.Unsynced(ctx, tt.limit)
			if err != nil {
				t.Fatalf("Unsynced() error = %v", err)
			}
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("Unsynced() returned %d rows, want %d: %#v", len(got), len(tt.wantIDs), got)
			}
			for i, want := range tt.wantIDs {
				if got[i].ID != want {
					t.Errorf("Unsynced()[%d].ID = %q, want %q", i, got[i].ID, want)
				}
			}
		})
	}
}

func TestStoreMarkSynced(t *testing.T) {
	base := time.Now().UTC()

	tests := []struct {
		name          string
		readings      []model.Reading
		markIDs       []string
		wantRemaining []string // remaining unsynced IDs, ascending timestamp order
	}{
		{
			name: "marking one ID removes only that row from Unsynced",
			readings: []model.Reading{
				{ID: "a", Source: "s", Tag: "t", Value: 1, Timestamp: base.Add(1 * time.Second)},
				{ID: "b", Source: "s", Tag: "t", Value: 2, Timestamp: base.Add(2 * time.Second)},
				{ID: "c", Source: "s", Tag: "t", Value: 3, Timestamp: base.Add(3 * time.Second)},
			},
			markIDs:       []string{"b"},
			wantRemaining: []string{"a", "c"},
		},
		{
			name: "marking all IDs empties Unsynced",
			readings: []model.Reading{
				{ID: "a", Source: "s", Tag: "t", Value: 1, Timestamp: base.Add(1 * time.Second)},
				{ID: "b", Source: "s", Tag: "t", Value: 2, Timestamp: base.Add(2 * time.Second)},
			},
			markIDs:       []string{"a", "b"},
			wantRemaining: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openTestStore(t)
			ctx := context.Background()

			for _, r := range tt.readings {
				if err := store.Put(ctx, r); err != nil {
					t.Fatalf("Put(%q) error = %v", r.ID, err)
				}
			}

			if err := store.MarkSynced(ctx, tt.markIDs); err != nil {
				t.Fatalf("MarkSynced() error = %v", err)
			}

			got, err := store.Unsynced(ctx, 100)
			if err != nil {
				t.Fatalf("Unsynced() error = %v", err)
			}
			if len(got) != len(tt.wantRemaining) {
				t.Fatalf("Unsynced() returned %d rows, want %d: %#v", len(got), len(tt.wantRemaining), got)
			}
			for i, want := range tt.wantRemaining {
				if got[i].ID != want {
					t.Errorf("Unsynced()[%d].ID = %q, want %q", i, got[i].ID, want)
				}
			}
		})
	}
}

func TestStoreRecent(t *testing.T) {
	base := time.Now().UTC()

	allReadings := []model.Reading{
		{ID: "t1-old", Source: "s", Tag: "temp", Value: 1, Timestamp: base.Add(1 * time.Second)},
		{ID: "t1-mid", Source: "s", Tag: "temp", Value: 2, Timestamp: base.Add(2 * time.Second)},
		{ID: "t1-new", Source: "s", Tag: "temp", Value: 3, Timestamp: base.Add(3 * time.Second)},
		{ID: "t2-mid", Source: "s", Tag: "pressure", Value: 4, Timestamp: base.Add(2 * time.Second)},
	}

	tests := []struct {
		name    string
		tag     string
		since   time.Time
		wantIDs []string
	}{
		{
			name:    "filters by tag only, all since the beginning",
			tag:     "temp",
			since:   base,
			wantIDs: []string{"t1-old", "t1-mid", "t1-new"},
		},
		{
			name:    "filters by tag and since, ascending order",
			tag:     "temp",
			since:   base.Add(2 * time.Second),
			wantIDs: []string{"t1-mid", "t1-new"},
		},
		{
			name:    "different tag returns only its own rows",
			tag:     "pressure",
			since:   base,
			wantIDs: []string{"t2-mid"},
		},
		{
			name:    "since after all timestamps returns nothing",
			tag:     "temp",
			since:   base.Add(10 * time.Second),
			wantIDs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openTestStore(t)
			ctx := context.Background()

			for _, r := range allReadings {
				if err := store.Put(ctx, r); err != nil {
					t.Fatalf("Put(%q) error = %v", r.ID, err)
				}
			}

			got, err := store.Recent(ctx, tt.tag, tt.since.Format(timeLayout))
			if err != nil {
				t.Fatalf("Recent() error = %v", err)
			}
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("Recent() returned %d rows, want %d: %#v", len(got), len(tt.wantIDs), got)
			}
			for i, want := range tt.wantIDs {
				if got[i].ID != want {
					t.Errorf("Recent()[%d].ID = %q, want %q", i, got[i].ID, want)
				}
			}
		})
	}
}

func TestStorePutAlarmUpsertOnClear(t *testing.T) {
	base := time.Now().UTC()

	tests := []struct {
		name string
		// raise, then optionally clear (nil = only raise this test)
		raise      model.Alarm
		clear      *time.Time
		wantRows   int
		wantSynced bool // whether the row should still show up in UnsyncedAlarms after the sequence
	}{
		{
			name: "raise only: row is unsynced with cleared_at nil",
			raise: model.Alarm{
				ID: "a1", Tag: "temp", Condition: ">", Value: 90, RaisedAt: base,
			},
			wantRows:   1,
			wantSynced: true,
		},
		{
			name: "raise then clear: same row updated, cleared_at set, re-armed unsynced",
			raise: model.Alarm{
				ID: "a2", Tag: "temp", Condition: ">", Value: 90, RaisedAt: base,
			},
			clear:      timePtr(base.Add(time.Minute)),
			wantRows:   1,
			wantSynced: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openTestStore(t)
			ctx := context.Background()

			if err := store.PutAlarm(ctx, tt.raise); err != nil {
				t.Fatalf("PutAlarm(raise) error = %v", err)
			}

			if tt.clear != nil {
				// mark synced first, to prove the clear resets it
				if err := store.MarkAlarmsSynced(ctx, []string{tt.raise.ID}); err != nil {
					t.Fatalf("MarkAlarmsSynced() error = %v", err)
				}
				cleared := tt.raise
				cleared.ClearedAt = tt.clear
				if err := store.PutAlarm(ctx, cleared); err != nil {
					t.Fatalf("PutAlarm(clear) error = %v", err)
				}
			}

			got, err := store.UnsyncedAlarms(ctx, 100)
			if err != nil {
				t.Fatalf("UnsyncedAlarms() error = %v", err)
			}
			if tt.wantSynced {
				if len(got) != tt.wantRows {
					t.Fatalf("UnsyncedAlarms() returned %d rows, want %d: %#v", len(got), tt.wantRows, got)
				}
				if got[0].ID != tt.raise.ID {
					t.Errorf("UnsyncedAlarms()[0].ID = %q, want %q", got[0].ID, tt.raise.ID)
				}
				if tt.clear != nil {
					if got[0].ClearedAt == nil {
						t.Fatalf("UnsyncedAlarms()[0].ClearedAt = nil, want %v", *tt.clear)
					}
					if !got[0].ClearedAt.Equal(*tt.clear) {
						t.Errorf("UnsyncedAlarms()[0].ClearedAt = %v, want %v", *got[0].ClearedAt, *tt.clear)
					}
				} else if got[0].ClearedAt != nil {
					t.Errorf("UnsyncedAlarms()[0].ClearedAt = %v, want nil", *got[0].ClearedAt)
				}
			}
		})
	}
}

func timePtr(t time.Time) *time.Time { return &t }

func TestStoreUnsyncedAlarmsAndMarkAlarmsSynced(t *testing.T) {
	base := time.Now().UTC()

	alarms := []model.Alarm{
		{ID: "a", Tag: "temp", Condition: ">", Value: 1, RaisedAt: base.Add(2 * time.Second)},
		{ID: "b", Tag: "temp", Condition: ">", Value: 2, RaisedAt: base.Add(1 * time.Second)},
		{ID: "c", Tag: "pressure", Condition: "<", Value: 3, RaisedAt: base.Add(3 * time.Second)},
	}

	store := openTestStore(t)
	ctx := context.Background()

	for _, a := range alarms {
		if err := store.PutAlarm(ctx, a); err != nil {
			t.Fatalf("PutAlarm(%q) error = %v", a.ID, err)
		}
	}

	got, err := store.UnsyncedAlarms(ctx, 100)
	if err != nil {
		t.Fatalf("UnsyncedAlarms() error = %v", err)
	}
	wantOrder := []string{"b", "a", "c"} // ascending raised_at
	if len(got) != len(wantOrder) {
		t.Fatalf("UnsyncedAlarms() returned %d rows, want %d", len(got), len(wantOrder))
	}
	for i, want := range wantOrder {
		if got[i].ID != want {
			t.Errorf("UnsyncedAlarms()[%d].ID = %q, want %q", i, got[i].ID, want)
		}
	}

	if err := store.MarkAlarmsSynced(ctx, []string{"a", "c"}); err != nil {
		t.Fatalf("MarkAlarmsSynced() error = %v", err)
	}

	remaining, err := store.UnsyncedAlarms(ctx, 100)
	if err != nil {
		t.Fatalf("UnsyncedAlarms() error = %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != "b" {
		t.Fatalf("UnsyncedAlarms() after mark = %#v, want only %q", remaining, "b")
	}
}

func TestStorePutRollupIdempotent(t *testing.T) {
	base := time.Now().UTC()

	roll := model.Rollup{
		Tag: "temp", WindowStart: base, WindowEnd: base.Add(30 * time.Second),
		Min: 1, Max: 10, Avg: 5.5, Count: 4,
	}

	store := openTestStore(t)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if err := store.PutRollup(ctx, roll); err != nil {
			t.Fatalf("PutRollup() call %d error = %v", i, err)
		}
	}

	got, err := store.UnsyncedRollups(ctx, 100)
	if err != nil {
		t.Fatalf("UnsyncedRollups() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("UnsyncedRollups() returned %d rows, want 1: %#v", len(got), got)
	}
	if got[0].Tag != roll.Tag || !got[0].WindowStart.Equal(roll.WindowStart) {
		t.Errorf("UnsyncedRollups()[0] = %#v, want tag=%q windowStart=%v", got[0], roll.Tag, roll.WindowStart)
	}
}

func TestStoreUnsyncedRollupsAndMarkRollupsSynced(t *testing.T) {
	base := time.Now().UTC()

	rollups := []model.Rollup{
		{Tag: "temp", WindowStart: base.Add(2 * time.Second), WindowEnd: base.Add(32 * time.Second), Min: 1, Max: 2, Avg: 1.5, Count: 2},
		{Tag: "temp", WindowStart: base.Add(1 * time.Second), WindowEnd: base.Add(31 * time.Second), Min: 1, Max: 2, Avg: 1.5, Count: 2},
		{Tag: "pressure", WindowStart: base.Add(3 * time.Second), WindowEnd: base.Add(33 * time.Second), Min: 1, Max: 2, Avg: 1.5, Count: 2},
	}

	store := openTestStore(t)
	ctx := context.Background()

	for _, r := range rollups {
		if err := store.PutRollup(ctx, r); err != nil {
			t.Fatalf("PutRollup(%q, %v) error = %v", r.Tag, r.WindowStart, err)
		}
	}

	got, err := store.UnsyncedRollups(ctx, 100)
	if err != nil {
		t.Fatalf("UnsyncedRollups() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("UnsyncedRollups() returned %d rows, want 3", len(got))
	}
	// ascending window_start: temp@+1s, temp@+2s, pressure@+3s
	if !got[0].WindowStart.Equal(rollups[1].WindowStart) || !got[1].WindowStart.Equal(rollups[0].WindowStart) || !got[2].WindowStart.Equal(rollups[2].WindowStart) {
		t.Fatalf("UnsyncedRollups() not ordered by window_start ascending: %#v", got)
	}

	if err := store.MarkRollupsSynced(ctx, []model.Rollup{rollups[0], rollups[2]}); err != nil {
		t.Fatalf("MarkRollupsSynced() error = %v", err)
	}

	remaining, err := store.UnsyncedRollups(ctx, 100)
	if err != nil {
		t.Fatalf("UnsyncedRollups() error = %v", err)
	}
	if len(remaining) != 1 || !remaining[0].WindowStart.Equal(rollups[1].WindowStart) {
		t.Fatalf("UnsyncedRollups() after mark = %#v, want only the %v window", remaining, rollups[1].WindowStart)
	}
}
