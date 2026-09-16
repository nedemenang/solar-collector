package sink

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"solar-collector/internal/model"
)

// fakeBatchResults is a hand-rolled stand-in for pgx.BatchResults. It can be
// configured to succeed for a number of Exec calls and then fail at a given
// call index, and to fail on Close, so Write's partial-batch and
// close-error handling can be exercised without a real connection.
type fakeBatchResults struct {
	// execErrAt is the zero-based Exec call index that should return
	// execErr. A negative value means every Exec call succeeds.
	execErrAt int
	execErr   error

	// closeErr is returned by every call to Close.
	closeErr error

	execCalls  int
	closeCalls int
}

func (f *fakeBatchResults) Exec() (pgconn.CommandTag, error) {
	idx := f.execCalls
	f.execCalls++
	if f.execErrAt >= 0 && idx == f.execErrAt {
		return pgconn.CommandTag{}, f.execErr
	}
	return pgconn.CommandTag{}, nil
}

func (f *fakeBatchResults) Query() (pgx.Rows, error) {
	return nil, nil
}

func (f *fakeBatchResults) QueryRow() pgx.Row {
	return nil
}

func (f *fakeBatchResults) Close() error {
	f.closeCalls++
	return f.closeErr
}

// fakeBatcher is a hand-rolled stand-in for pgxBatcher. It records the batch
// it was handed and returns a preconfigured pgx.BatchResults.
type fakeBatcher struct {
	results pgx.BatchResults

	sendBatchCalls int
	lastBatchLen   int
}

func (f *fakeBatcher) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	f.sendBatchCalls++
	f.lastBatchLen = b.Len()
	return f.results
}

func (f *fakeBatcher) Close() {}

func makeReadings(n int) []model.Reading {
	readings := make([]model.Reading, n)
	for i := range readings {
		readings[i] = model.Reading{
			ID:        "id",
			Source:    "source",
			Tag:       "tag",
			Value:     float64(i),
			Timestamp: time.Unix(0, 0),
		}
	}
	return readings
}

func makeAlarms(n int) []model.Alarm {
	alarms := make([]model.Alarm, n)
	for i := range alarms {
		alarms[i] = model.Alarm{
			ID:        "id",
			Tag:       "tag",
			Condition: ">",
			Value:     float64(i),
			RaisedAt:  time.Unix(0, 0),
		}
	}
	return alarms
}

func makeRollups(n int) []model.Rollup {
	rollups := make([]model.Rollup, n)
	for i := range rollups {
		rollups[i] = model.Rollup{
			Tag:         "tag",
			WindowStart: time.Unix(0, 0),
			WindowEnd:   time.Unix(30, 0),
			Min:         0,
			Max:         float64(i),
			Avg:         float64(i) / 2,
			Count:       i + 1,
		}
	}
	return rollups
}

func TestPostgresWrite(t *testing.T) {
	errBoom := errors.New("boom")
	errCloseBoom := errors.New("close boom")

	tests := []struct {
		name string

		numReadings int
		execErrAt   int // -1 means no Exec error
		execErr     error
		closeErr    error

		wantErr           error
		wantExecCalls     int
		wantCloseCalls    int
		wantSendBatchCall int
		wantBatchLen      int
	}{
		{
			name:              "empty readings",
			numReadings:       0,
			execErrAt:         -1,
			wantErr:           nil,
			wantExecCalls:     0,
			wantCloseCalls:    2, // explicit `return br.Close()` + deferred br.Close()
			wantSendBatchCall: 1,
			wantBatchLen:      0,
		},
		{
			name:              "all readings succeed",
			numReadings:       3,
			execErrAt:         -1,
			wantErr:           nil,
			wantExecCalls:     3,
			wantCloseCalls:    2, // explicit `return br.Close()` + deferred br.Close()
			wantSendBatchCall: 1,
			wantBatchLen:      3,
		},
		{
			name:              "exec fails partway through batch",
			numReadings:       5,
			execErrAt:         2,
			execErr:           errBoom,
			wantErr:           errBoom,
			wantExecCalls:     3, // indices 0, 1 succeed, index 2 fails and loop stops
			wantCloseCalls:    1, // only the deferred Close runs on the early return
			wantSendBatchCall: 1,
			wantBatchLen:      5,
		},
		{
			name:              "close fails after all execs succeed",
			numReadings:       2,
			execErrAt:         -1,
			closeErr:          errCloseBoom,
			wantErr:           errCloseBoom,
			wantExecCalls:     2,
			wantCloseCalls:    2, // explicit `return br.Close()` + deferred br.Close()
			wantSendBatchCall: 1,
			wantBatchLen:      2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := &fakeBatchResults{
				execErrAt: tt.execErrAt,
				execErr:   tt.execErr,
				closeErr:  tt.closeErr,
			}
			batcher := &fakeBatcher{results: results}
			p := &Postgres{pool: batcher}

			err := p.Write(context.Background(), makeReadings(tt.numReadings))

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Write() error = %v, want %v", err, tt.wantErr)
			}
			if results.execCalls != tt.wantExecCalls {
				t.Errorf("Exec called %d times, want %d", results.execCalls, tt.wantExecCalls)
			}
			if results.closeCalls != tt.wantCloseCalls {
				t.Errorf("Close called %d times, want %d", results.closeCalls, tt.wantCloseCalls)
			}
			if batcher.sendBatchCalls != tt.wantSendBatchCall {
				t.Errorf("SendBatch called %d times, want %d", batcher.sendBatchCalls, tt.wantSendBatchCall)
			}
			if batcher.lastBatchLen != tt.wantBatchLen {
				t.Errorf("batch queued %d queries, want %d", batcher.lastBatchLen, tt.wantBatchLen)
			}
		})
	}
}

func TestPostgresWriteAlarms(t *testing.T) {
	errBoom := errors.New("boom")
	errCloseBoom := errors.New("close boom")

	tests := []struct {
		name string

		numAlarms int
		execErrAt int // -1 means no Exec error
		execErr   error
		closeErr  error

		wantErr           error
		wantExecCalls     int
		wantCloseCalls    int
		wantSendBatchCall int
		wantBatchLen      int
	}{
		{
			name:              "empty alarms",
			numAlarms:         0,
			execErrAt:         -1,
			wantErr:           nil,
			wantExecCalls:     0,
			wantCloseCalls:    2,
			wantSendBatchCall: 1,
			wantBatchLen:      0,
		},
		{
			name:              "all alarms succeed",
			numAlarms:         3,
			execErrAt:         -1,
			wantErr:           nil,
			wantExecCalls:     3,
			wantCloseCalls:    2,
			wantSendBatchCall: 1,
			wantBatchLen:      3,
		},
		{
			name:              "exec fails partway through batch",
			numAlarms:         5,
			execErrAt:         2,
			execErr:           errBoom,
			wantErr:           errBoom,
			wantExecCalls:     3,
			wantCloseCalls:    1,
			wantSendBatchCall: 1,
			wantBatchLen:      5,
		},
		{
			name:              "close fails after all execs succeed",
			numAlarms:         2,
			execErrAt:         -1,
			closeErr:          errCloseBoom,
			wantErr:           errCloseBoom,
			wantExecCalls:     2,
			wantCloseCalls:    2,
			wantSendBatchCall: 1,
			wantBatchLen:      2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := &fakeBatchResults{
				execErrAt: tt.execErrAt,
				execErr:   tt.execErr,
				closeErr:  tt.closeErr,
			}
			batcher := &fakeBatcher{results: results}
			p := &Postgres{pool: batcher}

			err := p.WriteAlarms(context.Background(), makeAlarms(tt.numAlarms))

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("WriteAlarms() error = %v, want %v", err, tt.wantErr)
			}
			if results.execCalls != tt.wantExecCalls {
				t.Errorf("Exec called %d times, want %d", results.execCalls, tt.wantExecCalls)
			}
			if results.closeCalls != tt.wantCloseCalls {
				t.Errorf("Close called %d times, want %d", results.closeCalls, tt.wantCloseCalls)
			}
			if batcher.sendBatchCalls != tt.wantSendBatchCall {
				t.Errorf("SendBatch called %d times, want %d", batcher.sendBatchCalls, tt.wantSendBatchCall)
			}
			if batcher.lastBatchLen != tt.wantBatchLen {
				t.Errorf("batch queued %d queries, want %d", batcher.lastBatchLen, tt.wantBatchLen)
			}
		})
	}
}

func TestPostgresWriteRollups(t *testing.T) {
	errBoom := errors.New("boom")
	errCloseBoom := errors.New("close boom")

	tests := []struct {
		name string

		numRollups int
		execErrAt  int // -1 means no Exec error
		execErr    error
		closeErr   error

		wantErr           error
		wantExecCalls     int
		wantCloseCalls    int
		wantSendBatchCall int
		wantBatchLen      int
	}{
		{
			name:              "empty rollups",
			numRollups:        0,
			execErrAt:         -1,
			wantErr:           nil,
			wantExecCalls:     0,
			wantCloseCalls:    2,
			wantSendBatchCall: 1,
			wantBatchLen:      0,
		},
		{
			name:              "all rollups succeed",
			numRollups:        3,
			execErrAt:         -1,
			wantErr:           nil,
			wantExecCalls:     3,
			wantCloseCalls:    2,
			wantSendBatchCall: 1,
			wantBatchLen:      3,
		},
		{
			name:              "exec fails partway through batch",
			numRollups:        5,
			execErrAt:         2,
			execErr:           errBoom,
			wantErr:           errBoom,
			wantExecCalls:     3,
			wantCloseCalls:    1,
			wantSendBatchCall: 1,
			wantBatchLen:      5,
		},
		{
			name:              "close fails after all execs succeed",
			numRollups:        2,
			execErrAt:         -1,
			closeErr:          errCloseBoom,
			wantErr:           errCloseBoom,
			wantExecCalls:     2,
			wantCloseCalls:    2,
			wantSendBatchCall: 1,
			wantBatchLen:      2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := &fakeBatchResults{
				execErrAt: tt.execErrAt,
				execErr:   tt.execErr,
				closeErr:  tt.closeErr,
			}
			batcher := &fakeBatcher{results: results}
			p := &Postgres{pool: batcher}

			err := p.WriteRollups(context.Background(), makeRollups(tt.numRollups))

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("WriteRollups() error = %v, want %v", err, tt.wantErr)
			}
			if results.execCalls != tt.wantExecCalls {
				t.Errorf("Exec called %d times, want %d", results.execCalls, tt.wantExecCalls)
			}
			if results.closeCalls != tt.wantCloseCalls {
				t.Errorf("Close called %d times, want %d", results.closeCalls, tt.wantCloseCalls)
			}
			if batcher.sendBatchCalls != tt.wantSendBatchCall {
				t.Errorf("SendBatch called %d times, want %d", batcher.sendBatchCalls, tt.wantSendBatchCall)
			}
			if batcher.lastBatchLen != tt.wantBatchLen {
				t.Errorf("batch queued %d queries, want %d", batcher.lastBatchLen, tt.wantBatchLen)
			}
		})
	}
}
