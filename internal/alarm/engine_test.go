package alarm

import (
	"testing"
	"time"

	"solar-collector/internal/model"
)

func TestRuleBreaches(t *testing.T) {
	tests := []struct {
		name      string
		rule      Rule
		value     float64
		wantBreach bool
	}{
		{
			name:       "greater-than operator, value above threshold",
			rule:       Rule{Operator: ">", Threshold: 10},
			value:      15,
			wantBreach: true,
		},
		{
			name:       "greater-than operator, value below threshold",
			rule:       Rule{Operator: ">", Threshold: 10},
			value:      5,
			wantBreach: false,
		},
		{
			name:       "greater-than operator, value equal to threshold",
			rule:       Rule{Operator: ">", Threshold: 10},
			value:      10,
			wantBreach: false,
		},
		{
			name:       "less-than operator, value below threshold",
			rule:       Rule{Operator: "<", Threshold: 10},
			value:      5,
			wantBreach: true,
		},
		{
			name:       "less-than operator, value above threshold",
			rule:       Rule{Operator: "<", Threshold: 10},
			value:      15,
			wantBreach: false,
		},
		{
			name:       "unknown operator always false",
			rule:       Rule{Operator: "!=", Threshold: 10},
			value:      999,
			wantBreach: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rule.breaches(tt.value); got != tt.wantBreach {
				t.Errorf("breaches(%v) = %v, want %v", tt.value, got, tt.wantBreach)
			}
		})
	}
}

// step describes one call to Engine.Evaluate and the assertions to make
// about its result.
type step struct {
	reading   model.Reading
	wantAlarm bool
	// checked only when wantAlarm is true
	wantTag       string
	wantCondition string
	wantValue     float64
	wantRaisedAt  time.Time
	// wantClearedAt is checked only when wantAlarm is true. Zero value
	// means the step expects a raise (ClearedAt == nil); a non-zero value
	// means the step expects a clear (ClearedAt equal to this).
	wantClearedAt time.Time
}

func TestEngineEvaluate(t *testing.T) {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		rules []Rule
		steps []step
	}{
		{
			name:  "first breaching reading raises an alarm",
			rules: []Rule{{Tag: "temp", Operator: ">", Threshold: 100}},
			steps: []step{
				{
					reading:       model.Reading{ID: "r1", Tag: "temp", Value: 150, Timestamp: baseTime},
					wantAlarm:     true,
					wantTag:       "temp",
					wantCondition: ">",
					wantValue:     150,
					wantRaisedAt:  baseTime,
				},
			},
		},
		{
			name:  "second breaching reading while active does not re-raise",
			rules: []Rule{{Tag: "temp", Operator: ">", Threshold: 100}},
			steps: []step{
				{
					reading:       model.Reading{ID: "r1", Tag: "temp", Value: 150, Timestamp: baseTime},
					wantAlarm:     true,
					wantTag:       "temp",
					wantCondition: ">",
					wantValue:     150,
					wantRaisedAt:  baseTime,
				},
				{
					reading:   model.Reading{ID: "r2", Tag: "temp", Value: 160, Timestamp: baseTime.Add(time.Minute)},
					wantAlarm: false,
				},
			},
		},
		{
			name:  "clearing removes active alarm allowing a later re-raise",
			rules: []Rule{{Tag: "temp", Operator: ">", Threshold: 100}},
			steps: []step{
				{
					reading:       model.Reading{ID: "r1", Tag: "temp", Value: 150, Timestamp: baseTime},
					wantAlarm:     true,
					wantTag:       "temp",
					wantCondition: ">",
					wantValue:     150,
					wantRaisedAt:  baseTime,
				},
				{
					// clears the breach: Evaluate returns the original alarm
					// (raise fields unchanged) with ClearedAt now set, and
					// removes it from active state
					reading:       model.Reading{ID: "r2", Tag: "temp", Value: 50, Timestamp: baseTime.Add(time.Minute)},
					wantAlarm:     true,
					wantTag:       "temp",
					wantCondition: ">",
					wantValue:     150,
					wantRaisedAt:  baseTime,
					wantClearedAt: baseTime.Add(time.Minute),
				},
				{
					// breaches again: since state was cleared, this raises anew
					reading:       model.Reading{ID: "r3", Tag: "temp", Value: 200, Timestamp: baseTime.Add(2 * time.Minute)},
					wantAlarm:     true,
					wantTag:       "temp",
					wantCondition: ">",
					wantValue:     200,
					wantRaisedAt:  baseTime.Add(2 * time.Minute),
				},
			},
		},
		{
			name:  "reading for tag with no matching rule always returns nil",
			rules: []Rule{{Tag: "temp", Operator: ">", Threshold: 100}},
			steps: []step{
				{
					reading:   model.Reading{ID: "r1", Tag: "pressure", Value: 999, Timestamp: baseTime},
					wantAlarm: false,
				},
				{
					reading:   model.Reading{ID: "r2", Tag: "pressure", Value: -999, Timestamp: baseTime.Add(time.Minute)},
					wantAlarm: false,
				},
			},
		},
		{
			name: "two tags with independent rules don't interfere",
			rules: []Rule{
				{Tag: "temp", Operator: ">", Threshold: 100},
				{Tag: "pressure", Operator: "<", Threshold: 10},
			},
			steps: []step{
				{
					// temp breaches and raises
					reading:       model.Reading{ID: "r1", Tag: "temp", Value: 150, Timestamp: baseTime},
					wantAlarm:     true,
					wantTag:       "temp",
					wantCondition: ">",
					wantValue:     150,
					wantRaisedAt:  baseTime,
				},
				{
					// pressure breaches independently and raises too
					reading:       model.Reading{ID: "r2", Tag: "pressure", Value: 5, Timestamp: baseTime.Add(time.Minute)},
					wantAlarm:     true,
					wantTag:       "pressure",
					wantCondition: "<",
					wantValue:     5,
					wantRaisedAt:  baseTime.Add(time.Minute),
				},
				{
					// temp still active, does not re-raise
					reading:   model.Reading{ID: "r3", Tag: "temp", Value: 160, Timestamp: baseTime.Add(2 * time.Minute)},
					wantAlarm: false,
				},
				{
					// pressure clears while temp remains active and unaffected
					reading:       model.Reading{ID: "r4", Tag: "pressure", Value: 20, Timestamp: baseTime.Add(3 * time.Minute)},
					wantAlarm:     true,
					wantTag:       "pressure",
					wantCondition: "<",
					wantValue:     5,
					wantRaisedAt:  baseTime.Add(time.Minute),
					wantClearedAt: baseTime.Add(3 * time.Minute),
				},
				{
					// temp still active despite pressure's state change
					reading:   model.Reading{ID: "r5", Tag: "temp", Value: 170, Timestamp: baseTime.Add(4 * time.Minute)},
					wantAlarm: false,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewEngine(tt.rules)
			for i, s := range tt.steps {
				got := e.Evaluate(s.reading)
				if s.wantAlarm {
					if got == nil {
						t.Fatalf("step %d: Evaluate() = nil, want alarm", i)
					}
					if got.Tag != s.wantTag {
						t.Errorf("step %d: Tag = %q, want %q", i, got.Tag, s.wantTag)
					}
					if got.Condition != s.wantCondition {
						t.Errorf("step %d: Condition = %q, want %q", i, got.Condition, s.wantCondition)
					}
					if got.Value != s.wantValue {
						t.Errorf("step %d: Value = %v, want %v", i, got.Value, s.wantValue)
					}
					if !got.RaisedAt.Equal(s.wantRaisedAt) {
						t.Errorf("step %d: RaisedAt = %v, want %v", i, got.RaisedAt, s.wantRaisedAt)
					}
					if s.wantClearedAt.IsZero() {
						if got.ClearedAt != nil {
							t.Errorf("step %d: ClearedAt = %v, want nil (raise)", i, *got.ClearedAt)
						}
					} else {
						if got.ClearedAt == nil {
							t.Fatalf("step %d: ClearedAt = nil, want %v (clear)", i, s.wantClearedAt)
						}
						if !got.ClearedAt.Equal(s.wantClearedAt) {
							t.Errorf("step %d: ClearedAt = %v, want %v", i, *got.ClearedAt, s.wantClearedAt)
						}
					}
				} else if got != nil {
					t.Fatalf("step %d: Evaluate() = %+v, want nil", i, got)
				}
			}
		})
	}
}
