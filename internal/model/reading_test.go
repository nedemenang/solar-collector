package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAlarmClearedAtOmitEmpty(t *testing.T) {
	raisedAt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	clearedAt := time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		alarm         Alarm
		wantSubstring string // must be present in the JSON output
		wantAbsent    string // must NOT be present in the JSON output
	}{
		{
			name: "nil ClearedAt is omitted",
			alarm: Alarm{
				ID:        "a1",
				Tag:       "temp",
				Condition: ">",
				Value:     150,
				RaisedAt:  raisedAt,
				ClearedAt: nil,
			},
			wantAbsent: `"cleared_at"`,
		},
		{
			name: "non-nil ClearedAt is present",
			alarm: Alarm{
				ID:        "a1",
				Tag:       "temp",
				Condition: ">",
				Value:     150,
				RaisedAt:  raisedAt,
				ClearedAt: &clearedAt,
			},
			wantSubstring: `"cleared_at"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := json.Marshal(tt.alarm)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			s := string(out)

			if tt.wantSubstring != "" && !strings.Contains(s, tt.wantSubstring) {
				t.Errorf("json output %q does not contain %q", s, tt.wantSubstring)
			}
			if tt.wantAbsent != "" && strings.Contains(s, tt.wantAbsent) {
				t.Errorf("json output %q unexpectedly contains %q", s, tt.wantAbsent)
			}
		})
	}
}
