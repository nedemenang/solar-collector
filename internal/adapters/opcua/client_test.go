package opcua

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gopcua/opcua/ua"

	"solar-collector/internal/model"
)

func TestToFloat64(t *testing.T) {
	tests := []struct {
		name   string
		in     any
		want   float64
		wantOK bool
	}{
		{"float64", float64(3.14), 3.14, true},
		{"float32", float32(2.5), 2.5, true},
		{"int32", int32(42), 42, true},
		{"int64", int64(-7), -7, true},
		{"unsupported string", "not a number", 0, false},
		{"unsupported bool", true, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := toFloat64(tt.in)
			if ok != tt.wantOK {
				t.Fatalf("toFloat64(%v) ok = %v, want %v", tt.in, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Fatalf("toFloat64(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// fakeUAClient is a hand-rolled uaClient for tests. Behavior is configured
// via function fields; call counters track invocations.
type fakeUAClient struct {
	connectFn func(ctx context.Context) error
	closeFn   func(ctx context.Context) error
	readFn    func(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error)

	connectCalls int32
	closeCalls   int32
	readCalls    int32
}

func (f *fakeUAClient) Connect(ctx context.Context) error {
	atomic.AddInt32(&f.connectCalls, 1)
	if f.connectFn != nil {
		return f.connectFn(ctx)
	}
	return nil
}

func (f *fakeUAClient) Close(ctx context.Context) error {
	atomic.AddInt32(&f.closeCalls, 1)
	if f.closeFn != nil {
		return f.closeFn(ctx)
	}
	return nil
}

func (f *fakeUAClient) Read(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error) {
	atomic.AddInt32(&f.readCalls, 1)
	if f.readFn != nil {
		return f.readFn(ctx, req)
	}
	return &ua.ReadResponse{}, nil
}

// dataValueOK builds a *ua.DataValue with StatusOK wrapping v.
func dataValueOK(v any) *ua.DataValue {
	return &ua.DataValue{
		Status: ua.StatusOK,
		Value:  ua.MustVariant(v),
	}
}

// dataValueBadStatus builds a *ua.DataValue with a bad status but a valid
// value, to confirm pollOnce skips it based on Status alone.
func dataValueBadStatus(v any) *ua.DataValue {
	return &ua.DataValue{
		Status: ua.StatusBad,
		Value:  ua.MustVariant(v),
	}
}

func TestPollOnce(t *testing.T) {
	tags := []string{"PanelTemp", "Irradiance", "InverterPower"}
	nodeIDs := make([]*ua.NodeID, len(tags))
	for i, tag := range tags {
		id, err := ua.ParseNodeID("ns=1;s=" + tag)
		if err != nil {
			t.Fatalf("parse node id: %v", err)
		}
		nodeIDs[i] = id
	}

	tests := []struct {
		name       string
		results    []*ua.DataValue
		wantTags   []string
		wantValues []float64
	}{
		{
			name: "all ok and convertible",
			results: []*ua.DataValue{
				dataValueOK(float64(25.5)),
				dataValueOK(float32(800.0)),
				dataValueOK(int32(1500)),
			},
			wantTags:   []string{"PanelTemp", "Irradiance", "InverterPower"},
			wantValues: []float64{25.5, 800.0, 1500.0},
		},
		{
			name: "bad status skipped",
			results: []*ua.DataValue{
				dataValueOK(float64(25.5)),
				dataValueBadStatus(float64(999.0)),
				dataValueOK(int32(1500)),
			},
			wantTags:   []string{"PanelTemp", "InverterPower"},
			wantValues: []float64{25.5, 1500.0},
		},
		{
			name: "nil value skipped",
			results: []*ua.DataValue{
				dataValueOK(float64(25.5)),
				{Status: ua.StatusOK, Value: nil},
				dataValueOK(int32(1500)),
			},
			wantTags:   []string{"PanelTemp", "InverterPower"},
			wantValues: []float64{25.5, 1500.0},
		},
		{
			name: "unsupported value type skipped",
			results: []*ua.DataValue{
				dataValueOK(float64(25.5)),
				dataValueOK("not a number"),
				dataValueOK(int32(1500)),
			},
			wantTags:   []string{"PanelTemp", "InverterPower"},
			wantValues: []float64{25.5, 1500.0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			p := &Poller{tags: tags}
			c := &fakeUAClient{
				readFn: func(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error) {
					return &ua.ReadResponse{Results: tt.results}, nil
				},
			}

			out := make(chan model.Reading, len(tt.results))
			if err := p.pollOnce(ctx, c, nodeIDs, out); err != nil {
				t.Fatalf("pollOnce returned error: %v", err)
			}
			close(out)

			var gotTags []string
			var gotValues []float64
			for r := range out {
				if r.Source != "opcua" {
					t.Errorf("reading Source = %q, want %q", r.Source, "opcua")
				}
				gotTags = append(gotTags, r.Tag)
				gotValues = append(gotValues, r.Value)
			}

			if len(gotTags) != len(tt.wantTags) {
				t.Fatalf("got %d readings, want %d (tags=%v)", len(gotTags), len(tt.wantTags), gotTags)
			}
			for i := range tt.wantTags {
				if gotTags[i] != tt.wantTags[i] {
					t.Errorf("reading[%d].Tag = %q, want %q", i, gotTags[i], tt.wantTags[i])
				}
				if gotValues[i] != tt.wantValues[i] {
					t.Errorf("reading[%d].Value = %v, want %v", i, gotValues[i], tt.wantValues[i])
				}
			}
		})
	}
}

// TestPollOnceCancelledContext confirms pollOnce returns promptly when ctx
// is already cancelled and out is unbuffered/full, instead of blocking on
// the send forever.
func TestPollOnceCancelledContext(t *testing.T) {
	safety, cancelSafety := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelSafety()

	ctx, cancel := context.WithCancel(safety)
	cancel() // cancel immediately, before pollOnce runs

	tags := []string{"PanelTemp"}
	nodeIDs := make([]*ua.NodeID, 1)
	id, err := ua.ParseNodeID("ns=1;s=PanelTemp")
	if err != nil {
		t.Fatalf("parse node id: %v", err)
	}
	nodeIDs[0] = id

	p := &Poller{tags: tags}
	c := &fakeUAClient{
		readFn: func(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error) {
			return &ua.ReadResponse{Results: []*ua.DataValue{dataValueOK(float64(1.0))}}, nil
		},
	}

	// Unbuffered so a naive implementation would block forever on the send.
	out := make(chan model.Reading)

	done := make(chan error, 1)
	go func() {
		done <- p.pollOnce(ctx, c, nodeIDs, out)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("pollOnce returned error: %v", err)
		}
	case <-safety.Done():
		t.Fatal("pollOnce did not return promptly with a cancelled context and full/unbuffered out channel")
	}
}

// TestRunReconnects confirms Run retries after newClient fails, instead of
// giving up after the first failure.
func TestRunReconnects(t *testing.T) {
	var calls int32

	p := &Poller{
		tags:           []string{"PanelTemp"},
		interval:       time.Millisecond,
		reconnectDelay: time.Millisecond,
		newClient: func(endpoint string) (uaClient, error) {
			n := atomic.AddInt32(&calls, 1)
			if n == 1 {
				return nil, errors.New("simulated dial failure")
			}
			return &fakeUAClient{
				readFn: func(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error) {
					<-ctx.Done()
					return &ua.ReadResponse{}, ctx.Err()
				},
			}, nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	out := make(chan model.Reading, 8)
	if err := p.Run(ctx, out); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if got := atomic.LoadInt32(&calls); got < 2 {
		t.Fatalf("newClient called %d times, want at least 2 (should retry after failure)", got)
	}
}
