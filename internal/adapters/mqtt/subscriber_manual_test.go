//go:build manual

package mqtt

import (
	"context"
	"testing"
	"time"

	"solar-collector/internal/model"
)

func TestSubscriberAgainstLiveBroker(t *testing.T) {
	s := NewSubscriber("tcp://localhost:1883", "spBv1.0/site1/DDATA/gateway1")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out := make(chan model.Reading, 32)
	go s.Run(ctx, out)

	seen := map[string]bool{}
	for {
		select {
		case r := <-out:
			t.Logf("got reading: tag=%s value=%f timestamp=%s", r.Tag, r.Value, r.Timestamp)
			seen[r.Tag] = true
		case <-ctx.Done():
			if len(seen) == 0 {
				t.Fatal("never decoded a single sparkplug metric from the live broker")
			}
			return
		}
	}
}
