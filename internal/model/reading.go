package model

import "time"

type Reading struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`
	Tag       string    `json:"tag"`
	Value     float64   `json:"value"`
	Timestamp time.Time `json:"timestamp"`
}

type Alarm struct {
	ID         string    `json:"id"`
	Tag        string    `json:"tag"`
	Condition  string    `json:"condition"`
	Value      float64   `json:"value"`
	RaisedAt   time.Time `json:"raised_at"`
	ClearedAt  *time.Time `json:"cleared_at,omitempty"`
}

type Rollup struct {
	Tag         string    `json:"tag"`
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	Min         float64   `json:"min"`
	Max         float64   `json:"max"`
	Avg         float64   `json:"avg"`
	Count       int       `json:"count"`
}
