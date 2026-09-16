package rollup

import (
	"sync"
	"time"

	"solar-collector/internal/model"
)

// Aggregator computes per-tag min/max/avg over a fixed window, entirely
// in memory, so it keeps working even while the collector is disconnected
// from the cloud sink.
type Aggregator struct {
	window time.Duration
	mu     sync.Mutex
	bins   map[string]*bin
}

type bin struct {
	start time.Time
	min   float64
	max   float64
	sum   float64
	count int
}

func NewAggregator(window time.Duration) *Aggregator {
	return &Aggregator{window: window, bins: make(map[string]*bin)}
}

// Add returns a completed Rollup when the reading starts a new window for
// its tag, and nil while the current window is still filling up.
func (a *Aggregator) Add(r model.Reading) *model.Rollup {
	a.mu.Lock()
	defer a.mu.Unlock()

	b, ok := a.bins[r.Tag]
	if !ok {
		a.bins[r.Tag] = newBin(r)
		return nil
	}

	if r.Timestamp.Sub(b.start) >= a.window {
		completed := b.toRollup(r.Tag, r.Timestamp)
		a.bins[r.Tag] = newBin(r)
		return &completed
	}

	b.min = min(b.min, r.Value)
	b.max = max(b.max, r.Value)
	b.sum += r.Value
	b.count++
	return nil
}

func newBin(r model.Reading) *bin {
	return &bin{start: r.Timestamp, min: r.Value, max: r.Value, sum: r.Value, count: 1}
}

func (b *bin) toRollup(tag string, windowEnd time.Time) model.Rollup {
	return model.Rollup{
		Tag:         tag,
		WindowStart: b.start,
		WindowEnd:   windowEnd,
		Min:         b.min,
		Max:         b.max,
		Avg:         b.sum / float64(b.count),
		Count:       b.count,
	}
}
