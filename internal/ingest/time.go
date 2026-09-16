package ingest

import "time"

const timeLayout = time.RFC3339Nano

func parseTime(s string) (time.Time, error) {
	return time.Parse(timeLayout, s)
}
