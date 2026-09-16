package ingest

import (
	"testing"
	"time"
)

func TestParseTime(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Time
		wantErr bool
	}{
		{
			name:  "valid RFC3339Nano string",
			input: "2024-03-15T10:30:00.123456789Z",
			want:  time.Date(2024, 3, 15, 10, 30, 0, 123456789, time.UTC),
		},
		{
			name:    "invalid string",
			input:   "not-a-timestamp",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTime(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseTime(%q) error = nil, want error; got time %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTime(%q) unexpected error: %v", tt.input, err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("parseTime(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
