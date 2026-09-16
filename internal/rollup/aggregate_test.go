package rollup

import (
	"testing"
	"time"

	"solar-collector/internal/model"
)

// rollupWant captures the fields of a *model.Rollup we care to assert.
type rollupWant struct {
	windowStart time.Time
	windowEnd   time.Time
	min         float64
	max         float64
	avg         float64
	count       int
}

// addStep describes one call to Aggregator.Add and the expected outcome.
type addStep struct {
	reading      model.Reading
	wantComplete bool
	want         rollupWant
}

func TestAggregatorAdd(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	window := 5 * time.Minute

	tests := []struct {
		name  string
		steps []addStep
	}{
		{
			name: "first reading opens a bin and returns nil",
			steps: []addStep{
				{
					reading:      model.Reading{Tag: "temp", Value: 10, Timestamp: base},
					wantComplete: false,
				},
			},
		},
		{
			name: "readings within window update stats and return nil",
			steps: []addStep{
				{reading: model.Reading{Tag: "temp", Value: 10, Timestamp: base}, wantComplete: false},
				{reading: model.Reading{Tag: "temp", Value: 20, Timestamp: base.Add(time.Minute)}, wantComplete: false},
				{reading: model.Reading{Tag: "temp", Value: 5, Timestamp: base.Add(2 * time.Minute)}, wantComplete: false},
			},
		},
		{
			name: "reading at or past window boundary completes the bin and starts a fresh one",
			steps: []addStep{
				{reading: model.Reading{Tag: "temp", Value: 10, Timestamp: base}, wantComplete: false},
				{reading: model.Reading{Tag: "temp", Value: 20, Timestamp: base.Add(time.Minute)}, wantComplete: false},
				{reading: model.Reading{Tag: "temp", Value: 5, Timestamp: base.Add(2 * time.Minute)}, wantComplete: false},
				{
					// exactly at the window boundary: base + 5m - base = 5m >= window
					reading:      model.Reading{Tag: "temp", Value: 100, Timestamp: base.Add(5 * time.Minute)},
					wantComplete: true,
					want: rollupWant{
						windowStart: base,
						windowEnd:   base.Add(5 * time.Minute),
						min:         5,
						max:         20,
						avg:         (10.0 + 20.0 + 5.0) / 3.0,
						count:       3,
					},
				},
				{
					// the reading that completed the previous bin starts a new one
					reading:      model.Reading{Tag: "temp", Value: 200, Timestamp: base.Add(6 * time.Minute)},
					wantComplete: false,
				},
				{
					// completes the fresh bin, which should only contain the
					// two most recent readings (100 and 200)
					reading:      model.Reading{Tag: "temp", Value: 1, Timestamp: base.Add(11 * time.Minute)},
					wantComplete: true,
					want: rollupWant{
						windowStart: base.Add(5 * time.Minute),
						windowEnd:   base.Add(11 * time.Minute),
						min:         100,
						max:         200,
						avg:         150,
						count:       2,
					},
				},
			},
		},
		{
			name: "two tags get independent bins",
			steps: []addStep{
				{reading: model.Reading{Tag: "temp", Value: 10, Timestamp: base}, wantComplete: false},
				{reading: model.Reading{Tag: "pressure", Value: 1000, Timestamp: base}, wantComplete: false},
				{reading: model.Reading{Tag: "temp", Value: 30, Timestamp: base.Add(time.Minute)}, wantComplete: false},
				{
					// completes temp's bin only
					reading:      model.Reading{Tag: "temp", Value: 50, Timestamp: base.Add(5 * time.Minute)},
					wantComplete: true,
					want: rollupWant{
						windowStart: base,
						windowEnd:   base.Add(5 * time.Minute),
						min:         10,
						max:         30,
						avg:         20,
						count:       2,
					},
				},
				{
					// pressure's bin is still independently accumulating
					reading:      model.Reading{Tag: "pressure", Value: 2000, Timestamp: base.Add(4 * time.Minute)},
					wantComplete: false,
				},
				{
					// completes pressure's bin, unaffected by temp's reset
					reading:      model.Reading{Tag: "pressure", Value: 3000, Timestamp: base.Add(5 * time.Minute)},
					wantComplete: true,
					want: rollupWant{
						windowStart: base,
						windowEnd:   base.Add(5 * time.Minute),
						min:         1000,
						max:         2000,
						avg:         1500,
						count:       2,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewAggregator(window)
			for i, s := range tt.steps {
				got := a.Add(s.reading)
				if s.wantComplete {
					if got == nil {
						t.Fatalf("step %d: Add() = nil, want completed rollup", i)
					}
					if !got.WindowStart.Equal(s.want.windowStart) {
						t.Errorf("step %d: WindowStart = %v, want %v", i, got.WindowStart, s.want.windowStart)
					}
					if !got.WindowEnd.Equal(s.want.windowEnd) {
						t.Errorf("step %d: WindowEnd = %v, want %v", i, got.WindowEnd, s.want.windowEnd)
					}
					if got.Min != s.want.min {
						t.Errorf("step %d: Min = %v, want %v", i, got.Min, s.want.min)
					}
					if got.Max != s.want.max {
						t.Errorf("step %d: Max = %v, want %v", i, got.Max, s.want.max)
					}
					if got.Avg != s.want.avg {
						t.Errorf("step %d: Avg = %v, want %v", i, got.Avg, s.want.avg)
					}
					if got.Count != s.want.count {
						t.Errorf("step %d: Count = %v, want %v", i, got.Count, s.want.count)
					}
				} else if got != nil {
					t.Fatalf("step %d: Add() = %+v, want nil", i, got)
				}
			}
		})
	}
}
