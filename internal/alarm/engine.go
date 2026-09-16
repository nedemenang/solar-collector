package alarm

import (
	"log/slog"
	"sync"

	"solar-collector/internal/model"
)

type Rule struct {
	Tag       string
	Operator  string // ">" or "<"
	Threshold float64
}

func (r Rule) breaches(value float64) bool {
	switch r.Operator {
	case ">":
		return value > r.Threshold
	case "<":
		return value < r.Threshold
	default:
		return false
	}
}

// Engine tracks one active alarm per tag so it raises on the first breach
// and clears once, instead of re-raising on every reading while a value
// stays out of range.
type Engine struct {
	rules  []Rule
	mu     sync.Mutex
	active map[string]model.Alarm
}

func NewEngine(rules []Rule) *Engine {
	return &Engine{rules: rules, active: make(map[string]model.Alarm)}
}

func (e *Engine) Evaluate(r model.Reading) *model.Alarm {
	for _, rule := range e.rules {
		if rule.Tag != r.Tag {
			continue
		}

		e.mu.Lock()
		active, isActive := e.active[r.Tag]
		breached := rule.breaches(r.Value)

		switch {
		case breached && !isActive:
			a := model.Alarm{
				ID:        r.ID,
				Tag:       r.Tag,
				Condition: rule.Operator,
				Value:     r.Value,
				RaisedAt:  r.Timestamp,
			}
			e.active[r.Tag] = a
			e.mu.Unlock()
			slog.Warn("alarm raised", "tag", r.Tag, "value", r.Value, "operator", rule.Operator, "threshold", rule.Threshold)
			return &a

		case !breached && isActive:
			delete(e.active, r.Tag)
			e.mu.Unlock()
			cleared := r.Timestamp
			active.ClearedAt = &cleared
			slog.Info("alarm cleared", "tag", r.Tag, "value", r.Value)
			return &active

		default:
			e.mu.Unlock()
			return nil
		}
	}
	return nil
}
