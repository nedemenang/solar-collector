package ingest

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"solar-collector/internal/model"
)

// fakeBufferStore is a hand-rolled fake implementing bufferStore, with
// configurable return values/errors and call-recording.
type fakeBufferStore struct {
	unsyncedReadings []model.Reading
	unsyncedErr      error
	markSyncedErr    error

	unsyncedAlarms      []model.Alarm
	unsyncedAlarmsErr   error
	markAlarmsSyncedErr error

	unsyncedRollups      []model.Rollup
	unsyncedRollupsErr   error
	markRollupsSyncedErr error

	markSyncedCalls        [][]string
	markAlarmsSyncedCalls  [][]string
	markRollupsSyncedCalls [][]model.Rollup
}

func (f *fakeBufferStore) Unsynced(ctx context.Context, limit int) ([]model.Reading, error) {
	if f.unsyncedErr != nil {
		return nil, f.unsyncedErr
	}
	return f.unsyncedReadings, nil
}

func (f *fakeBufferStore) MarkSynced(ctx context.Context, ids []string) error {
	f.markSyncedCalls = append(f.markSyncedCalls, ids)
	if f.markSyncedErr != nil {
		return f.markSyncedErr
	}
	return nil
}

func (f *fakeBufferStore) UnsyncedAlarms(ctx context.Context, limit int) ([]model.Alarm, error) {
	if f.unsyncedAlarmsErr != nil {
		return nil, f.unsyncedAlarmsErr
	}
	return f.unsyncedAlarms, nil
}

func (f *fakeBufferStore) MarkAlarmsSynced(ctx context.Context, ids []string) error {
	f.markAlarmsSyncedCalls = append(f.markAlarmsSyncedCalls, ids)
	if f.markAlarmsSyncedErr != nil {
		return f.markAlarmsSyncedErr
	}
	return nil
}

func (f *fakeBufferStore) UnsyncedRollups(ctx context.Context, limit int) ([]model.Rollup, error) {
	if f.unsyncedRollupsErr != nil {
		return nil, f.unsyncedRollupsErr
	}
	return f.unsyncedRollups, nil
}

func (f *fakeBufferStore) MarkRollupsSynced(ctx context.Context, rollups []model.Rollup) error {
	f.markRollupsSyncedCalls = append(f.markRollupsSyncedCalls, rollups)
	if f.markRollupsSyncedErr != nil {
		return f.markRollupsSyncedErr
	}
	return nil
}

// fakeSink is a hand-rolled fake implementing Sink, with configurable
// return errors and call-recording.
type fakeSink struct {
	writeErr       error
	writeAlarmsErr error
	writeRollupErr error

	writeCalls       [][]model.Reading
	writeAlarmsCalls [][]model.Alarm
	writeRollupCalls [][]model.Rollup
}

func (f *fakeSink) Write(ctx context.Context, readings []model.Reading) error {
	f.writeCalls = append(f.writeCalls, readings)
	if f.writeErr != nil {
		return f.writeErr
	}
	return nil
}

func (f *fakeSink) WriteAlarms(ctx context.Context, alarms []model.Alarm) error {
	f.writeAlarmsCalls = append(f.writeAlarmsCalls, alarms)
	if f.writeAlarmsErr != nil {
		return f.writeAlarmsErr
	}
	return nil
}

func (f *fakeSink) WriteRollups(ctx context.Context, rollups []model.Rollup) error {
	f.writeRollupCalls = append(f.writeRollupCalls, rollups)
	if f.writeRollupErr != nil {
		return f.writeRollupErr
	}
	return nil
}

func TestReplayerTick(t *testing.T) {
	errUnsynced := errors.New("unsynced boom")
	errWrite := errors.New("write boom")
	errMarkSynced := errors.New("mark synced boom")

	sampleReadings := []model.Reading{
		{ID: "a", Source: "src", Tag: "tag1", Value: 1.0},
		{ID: "b", Source: "src", Tag: "tag2", Value: 2.0},
	}

	tests := []struct {
		name string

		unsyncedReadings []model.Reading
		unsyncedErr      error
		writeErr         error
		markSyncedErr    error

		wantErr           error
		wantWriteCalls    [][]model.Reading
		wantMarkSyncedIDs [][]string
	}{
		{
			name:              "no unsynced rows: sink never called, tick returns nil",
			unsyncedReadings:  nil,
			wantErr:           nil,
			wantWriteCalls:    nil,
			wantMarkSyncedIDs: nil,
		},
		{
			name:              "rows present and write succeeds: write and mark synced called",
			unsyncedReadings:  sampleReadings,
			wantErr:           nil,
			wantWriteCalls:    [][]model.Reading{sampleReadings},
			wantMarkSyncedIDs: [][]string{{"a", "b"}},
		},
		{
			name:              "write fails: tick returns error, mark synced never called",
			unsyncedReadings:  sampleReadings,
			writeErr:          errWrite,
			wantErr:           errWrite,
			wantWriteCalls:    [][]model.Reading{sampleReadings},
			wantMarkSyncedIDs: nil,
		},
		{
			name:              "unsynced fails: tick returns error, write and mark synced never called",
			unsyncedErr:       errUnsynced,
			wantErr:           errUnsynced,
			wantWriteCalls:    nil,
			wantMarkSyncedIDs: nil,
		},
		{
			name:              "mark synced fails after successful write: tick returns that error",
			unsyncedReadings:  sampleReadings,
			markSyncedErr:     errMarkSynced,
			wantErr:           errMarkSynced,
			wantWriteCalls:    [][]model.Reading{sampleReadings},
			wantMarkSyncedIDs: [][]string{{"a", "b"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeBufferStore{
				unsyncedReadings: tt.unsyncedReadings,
				unsyncedErr:      tt.unsyncedErr,
				markSyncedErr:    tt.markSyncedErr,
			}
			sink := &fakeSink{writeErr: tt.writeErr}

			r := &Replayer{store: store, sink: sink, batch: 200}

			err := r.tick(context.Background())

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("tick() error = %v, want %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(sink.writeCalls, tt.wantWriteCalls) {
				t.Errorf("Write calls = %#v, want %#v", sink.writeCalls, tt.wantWriteCalls)
			}
			if !reflect.DeepEqual(store.markSyncedCalls, tt.wantMarkSyncedIDs) {
				t.Errorf("MarkSynced calls = %#v, want %#v", store.markSyncedCalls, tt.wantMarkSyncedIDs)
			}
		})
	}
}

func TestReplayerTickAlarms(t *testing.T) {
	errUnsynced := errors.New("unsynced alarms boom")
	errWrite := errors.New("write alarms boom")
	errMarkSynced := errors.New("mark alarms synced boom")

	sampleAlarms := []model.Alarm{
		{ID: "a", Tag: "temp", Condition: ">", Value: 1.0},
		{ID: "b", Tag: "pressure", Condition: "<", Value: 2.0},
	}

	tests := []struct {
		name string

		unsyncedAlarms      []model.Alarm
		unsyncedAlarmsErr   error
		writeAlarmsErr      error
		markAlarmsSyncedErr error

		wantErr           error
		wantWriteCalls    [][]model.Alarm
		wantMarkSyncedIDs [][]string
	}{
		{
			name:              "no unsynced alarms: sink never called, tick returns nil",
			unsyncedAlarms:    nil,
			wantErr:           nil,
			wantWriteCalls:    nil,
			wantMarkSyncedIDs: nil,
		},
		{
			name:              "rows present and write succeeds: write and mark synced called",
			unsyncedAlarms:    sampleAlarms,
			wantErr:           nil,
			wantWriteCalls:    [][]model.Alarm{sampleAlarms},
			wantMarkSyncedIDs: [][]string{{"a", "b"}},
		},
		{
			name:              "write fails: tick returns error, mark synced never called",
			unsyncedAlarms:    sampleAlarms,
			writeAlarmsErr:    errWrite,
			wantErr:           errWrite,
			wantWriteCalls:    [][]model.Alarm{sampleAlarms},
			wantMarkSyncedIDs: nil,
		},
		{
			name:              "unsynced fails: tick returns error, write and mark synced never called",
			unsyncedAlarmsErr: errUnsynced,
			wantErr:           errUnsynced,
			wantWriteCalls:    nil,
			wantMarkSyncedIDs: nil,
		},
		{
			name:                "mark synced fails after successful write: tick returns that error",
			unsyncedAlarms:      sampleAlarms,
			markAlarmsSyncedErr: errMarkSynced,
			wantErr:             errMarkSynced,
			wantWriteCalls:      [][]model.Alarm{sampleAlarms},
			wantMarkSyncedIDs:   [][]string{{"a", "b"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeBufferStore{
				unsyncedAlarms:      tt.unsyncedAlarms,
				unsyncedAlarmsErr:   tt.unsyncedAlarmsErr,
				markAlarmsSyncedErr: tt.markAlarmsSyncedErr,
			}
			sink := &fakeSink{writeAlarmsErr: tt.writeAlarmsErr}

			r := &Replayer{store: store, sink: sink, batch: 200}

			err := r.tick(context.Background())

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("tick() error = %v, want %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(sink.writeAlarmsCalls, tt.wantWriteCalls) {
				t.Errorf("WriteAlarms calls = %#v, want %#v", sink.writeAlarmsCalls, tt.wantWriteCalls)
			}
			if !reflect.DeepEqual(store.markAlarmsSyncedCalls, tt.wantMarkSyncedIDs) {
				t.Errorf("MarkAlarmsSynced calls = %#v, want %#v", store.markAlarmsSyncedCalls, tt.wantMarkSyncedIDs)
			}
		})
	}
}

func TestReplayerTickRollups(t *testing.T) {
	errUnsynced := errors.New("unsynced rollups boom")
	errWrite := errors.New("write rollups boom")
	errMarkSynced := errors.New("mark rollups synced boom")

	sampleRollups := []model.Rollup{
		{Tag: "temp", Min: 1, Max: 2, Avg: 1.5, Count: 2},
		{Tag: "pressure", Min: 3, Max: 4, Avg: 3.5, Count: 2},
	}

	tests := []struct {
		name string

		unsyncedRollups      []model.Rollup
		unsyncedRollupsErr   error
		writeRollupErr       error
		markRollupsSyncedErr error

		wantErr            error
		wantWriteCalls     [][]model.Rollup
		wantMarkSyncedRows [][]model.Rollup
	}{
		{
			name:               "no unsynced rollups: sink never called, tick returns nil",
			unsyncedRollups:    nil,
			wantErr:            nil,
			wantWriteCalls:     nil,
			wantMarkSyncedRows: nil,
		},
		{
			name:               "rows present and write succeeds: write and mark synced called",
			unsyncedRollups:    sampleRollups,
			wantErr:            nil,
			wantWriteCalls:     [][]model.Rollup{sampleRollups},
			wantMarkSyncedRows: [][]model.Rollup{sampleRollups},
		},
		{
			name:               "write fails: tick returns error, mark synced never called",
			unsyncedRollups:    sampleRollups,
			writeRollupErr:     errWrite,
			wantErr:            errWrite,
			wantWriteCalls:     [][]model.Rollup{sampleRollups},
			wantMarkSyncedRows: nil,
		},
		{
			name:               "unsynced fails: tick returns error, write and mark synced never called",
			unsyncedRollupsErr: errUnsynced,
			wantErr:            errUnsynced,
			wantWriteCalls:     nil,
			wantMarkSyncedRows: nil,
		},
		{
			name:                 "mark synced fails after successful write: tick returns that error",
			unsyncedRollups:      sampleRollups,
			markRollupsSyncedErr: errMarkSynced,
			wantErr:              errMarkSynced,
			wantWriteCalls:       [][]model.Rollup{sampleRollups},
			wantMarkSyncedRows:   [][]model.Rollup{sampleRollups},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeBufferStore{
				unsyncedRollups:      tt.unsyncedRollups,
				unsyncedRollupsErr:   tt.unsyncedRollupsErr,
				markRollupsSyncedErr: tt.markRollupsSyncedErr,
			}
			sink := &fakeSink{writeRollupErr: tt.writeRollupErr}

			r := &Replayer{store: store, sink: sink, batch: 200}

			err := r.tick(context.Background())

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("tick() error = %v, want %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(sink.writeRollupCalls, tt.wantWriteCalls) {
				t.Errorf("WriteRollups calls = %#v, want %#v", sink.writeRollupCalls, tt.wantWriteCalls)
			}
			if !reflect.DeepEqual(store.markRollupsSyncedCalls, tt.wantMarkSyncedRows) {
				t.Errorf("MarkRollupsSynced calls = %#v, want %#v", store.markRollupsSyncedCalls, tt.wantMarkSyncedRows)
			}
		})
	}
}

// TestReplayerTickIsolatesResources proves that one resource's replay
// failure doesn't block the other two from draining in the same tick —
// each of readings/alarms/rollups is drained unconditionally.
func TestReplayerTickIsolatesResources(t *testing.T) {
	errReadWrite := errors.New("readings write boom")

	sampleReadings := []model.Reading{{ID: "r1", Tag: "temp", Value: 1.0}}
	sampleAlarms := []model.Alarm{{ID: "a1", Tag: "temp", Condition: ">", Value: 2.0}}
	sampleRollups := []model.Rollup{{Tag: "temp", Min: 1, Max: 2, Avg: 1.5, Count: 2}}

	store := &fakeBufferStore{
		unsyncedReadings: sampleReadings,
		unsyncedAlarms:   sampleAlarms,
		unsyncedRollups:  sampleRollups,
	}
	sink := &fakeSink{writeErr: errReadWrite}

	r := &Replayer{store: store, sink: sink, batch: 200}

	err := r.tick(context.Background())

	if !errors.Is(err, errReadWrite) {
		t.Fatalf("tick() error = %v, want it to wrap %v", err, errReadWrite)
	}
	// readings failed to write, so its rows must not be marked synced
	if store.markSyncedCalls != nil {
		t.Errorf("MarkSynced calls = %#v, want none (readings write failed)", store.markSyncedCalls)
	}
	// alarms and rollups had no write error, so they must have completed
	// successfully in the same tick despite the readings failure
	if !reflect.DeepEqual(sink.writeAlarmsCalls, [][]model.Alarm{sampleAlarms}) {
		t.Errorf("WriteAlarms calls = %#v, want alarms to still be replayed", sink.writeAlarmsCalls)
	}
	if !reflect.DeepEqual(store.markAlarmsSyncedCalls, [][]string{{"a1"}}) {
		t.Errorf("MarkAlarmsSynced calls = %#v, want alarms still marked synced", store.markAlarmsSyncedCalls)
	}
	if !reflect.DeepEqual(sink.writeRollupCalls, [][]model.Rollup{sampleRollups}) {
		t.Errorf("WriteRollups calls = %#v, want rollups to still be replayed", sink.writeRollupCalls)
	}
	if !reflect.DeepEqual(store.markRollupsSyncedCalls, [][]model.Rollup{sampleRollups}) {
		t.Errorf("MarkRollupsSynced calls = %#v, want rollups still marked synced", store.markRollupsSyncedCalls)
	}
}
