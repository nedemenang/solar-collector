package mqtt

import (
	"strconv"
	"testing"
	"time"

	"github.com/weekaung/sparkplugb-client/sparkplug"

	"solar-collector/internal/model"
)

// encode builds a real Sparkplug B payload byte string by round-tripping
// through the actual codec used in production, rather than hand-crafting
// bytes. This is the highest-value test in the repo per the project's own
// stated testing philosophy: wire-format / protocol correctness.
//
// Note on Timestamp: the vendored codec's EncodePayload ignores whatever is
// in Payload.Timestamp and always stamps the wire payload with time.Now()
// at encode time, UNLESS isDeathPayload=true, in which case it omits the
// timestamp field from the wire entirely (leaving it unset, i.e. zero after
// decode). That's the only way to produce a zero-value Timestamp through
// the real codec, so the "falls back to time.Now()" case below encodes
// with isDeathPayload=true.
func encode(t *testing.T, isDeathPayload bool, metrics []sparkplug.Metric) []byte {
	t.Helper()
	p := &sparkplug.Payload{Metrics: metrics}
	raw, err := p.EncodePayload(isDeathPayload)
	if err != nil {
		t.Fatalf("EncodePayload() error = %v", err)
	}
	return raw
}

// decodeTimestamp independently decodes raw (same bytes handle() will
// decode) to learn what timestamp the wire payload actually carries, so
// tests can assert handle() propagates it faithfully without hardcoding
// a value the codec wouldn't have produced.
func decodeTimestamp(t *testing.T, raw []byte) time.Time {
	t.Helper()
	p := &sparkplug.Payload{}
	if err := p.DecodePayload(raw); err != nil {
		t.Fatalf("DecodePayload() error = %v", err)
	}
	return p.Timestamp
}

func TestHandle(t *testing.T) {
	tests := []struct {
		name        string
		raw         func(t *testing.T) []byte
		wantCount   int
		wantTags    []string
		wantValues  []float64
		wantExactTS bool // compare reading.Timestamp against the raw payload's own decoded timestamp
		wantNowish  bool // compare reading.Timestamp against time.Now().UTC() within tolerance
	}{
		{
			name: "single float metric with valid numeric value",
			raw: func(t *testing.T) []byte {
				return encode(t, false, []sparkplug.Metric{
					{Name: "AmbientTemp", DataType: sparkplug.TypeFloat, Value: "23.5"},
				})
			},
			wantCount:   1,
			wantTags:    []string{"AmbientTemp"},
			wantValues:  []float64{23.5},
			wantExactTS: true,
		},
		{
			name: "multiple float metrics produce multiple readings",
			raw: func(t *testing.T) []byte {
				return encode(t, false, []sparkplug.Metric{
					{Name: "AmbientTemp", DataType: sparkplug.TypeFloat, Value: "24.1"},
					{Name: "StringCurrent", DataType: sparkplug.TypeFloat, Value: "8.2"},
				})
			},
			wantCount:   2,
			wantTags:    []string{"AmbientTemp", "StringCurrent"},
			wantValues:  []float64{24.1, 8.2},
			wantExactTS: true,
		},
		{
			name: "double datatype metric decodes empty and is silently dropped",
			raw: func(t *testing.T) []byte {
				return encode(t, false, []sparkplug.Metric{
					{Name: "PanelTemp", DataType: sparkplug.DataType(10), Value: "23.5"},
				})
			},
			wantCount: 0,
		},
		{
			name: "zero-value timestamp falls back to time.Now().UTC()",
			raw: func(t *testing.T) []byte {
				// isDeathPayload=true is the only way this codec produces a
				// wire payload with no timestamp field set at all, which
				// decodes back to a zero-value time.Time.
				return encode(t, true, []sparkplug.Metric{
					{Name: "AmbientTemp", DataType: sparkplug.TypeFloat, Value: "25.0"},
				})
			},
			wantCount:  1,
			wantTags:   []string{"AmbientTemp"},
			wantValues: []float64{25.0},
			wantNowish: true,
		},
		{
			name: "malformed bytes do not panic and emit nothing",
			raw: func(t *testing.T) []byte {
				return []byte("this is not a valid sparkplug protobuf payload")
			},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Subscriber{broker: "tcp://unused:1883", topic: "unused"}

			raw := tt.raw(t)

			var expectedTS time.Time
			if tt.wantExactTS {
				expectedTS = decodeTimestamp(t, raw)
				if expectedTS.IsZero() {
					t.Fatal("test setup error: expected a non-zero decoded timestamp")
				}
			}

			// out is buffered generously so handle() -- which sends
			// synchronously with no ctx/select of its own -- never blocks,
			// including on the "malformed"/"dropped" cases where nothing
			// is sent at all.
			out := make(chan model.Reading, 16)

			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("handle() panicked: %v", r)
				}
			}()
			s.handle(raw, out)

			var got []model.Reading
		drain:
			for {
				select {
				case r := <-out:
					got = append(got, r)
				default:
					break drain
				}
			}

			if len(got) != tt.wantCount {
				t.Fatalf("got %d readings, want %d: %+v", len(got), tt.wantCount, got)
			}

			for i, r := range got {
				if r.Source != "mqtt" {
					t.Errorf("reading[%d].Source = %q, want %q", i, r.Source, "mqtt")
				}
				if i < len(tt.wantTags) && r.Tag != tt.wantTags[i] {
					t.Errorf("reading[%d].Tag = %q, want %q", i, r.Tag, tt.wantTags[i])
				}
				if i < len(tt.wantValues) && r.Value != tt.wantValues[i] {
					t.Errorf("reading[%d].Value = %v, want %v", i, r.Value, tt.wantValues[i])
				}
				if r.ID == "" {
					t.Errorf("reading[%d].ID is empty, want a generated UUID", i)
				}

				switch {
				case tt.wantExactTS:
					if !r.Timestamp.Equal(expectedTS) {
						t.Errorf("reading[%d].Timestamp = %v, want %v (payload's decoded timestamp)", i, r.Timestamp, expectedTS)
					}
				case tt.wantNowish:
					if delta := time.Since(r.Timestamp); delta < -5*time.Second || delta > 5*time.Second {
						t.Errorf("reading[%d].Timestamp = %v, not within 5s of now", i, r.Timestamp)
					}
					if r.Timestamp.Location() != time.UTC {
						t.Errorf("reading[%d].Timestamp location = %v, want UTC", i, r.Timestamp.Location())
					}
				}
			}
		})
	}
}

// TestParseFloatAssumption documents why a Double-datatype metric (whose
// Value decodes to "") is silently skipped by handle(): strconv.ParseFloat
// errors on an empty string, hitting the `continue` in handle()'s loop.
func TestParseFloatAssumption(t *testing.T) {
	if _, err := strconv.ParseFloat("", 64); err == nil {
		t.Fatal("expected empty string to fail ParseFloat, proving Double-typed metrics (empty Value) are skipped by handle")
	}
}

// Connection-lifecycle wiring (Connect/Subscribe/OnConnectHandler/
// reconnect behavior) is exercised by the existing `-tags manual` live
// broker test in subscriber_manual_test.go, which runs against a real
// simulator/broker. The actual wire-format risk in this codebase --
// decoding Sparkplug B payloads, including the Float-vs-Double gotcha --
// lives entirely in handle() and is covered by TestHandle above. Faking
// paho.Client/paho.Token well enough to drive Run() past Connect() without
// duplicating paho's own callback-registration semantics wasn't worth it
// here.
