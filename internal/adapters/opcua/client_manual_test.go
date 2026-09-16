//go:build manual

package opcua

import (
	"context"
	"testing"
	"time"

	"solar-collector/internal/model"
)

func TestPollerAgainstLiveSimulator(t *testing.T) {
	p := NewPoller("opc.tcp://localhost:4840/UA/SolarSite", []string{"PanelTemp", "Irradiance", "InverterPower", "DCVoltage"}, time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out := make(chan model.Reading, 32)
	go p.Run(ctx, out)

	seen := map[string]bool{}
	for {
		select {
		case r := <-out:
			t.Logf("got reading: tag=%s value=%f", r.Tag, r.Value)
			seen[r.Tag] = true
		case <-ctx.Done():
			for _, tag := range p.tags {
				if !seen[tag] {
					t.Errorf("never received a reading for tag %s", tag)
				}
			}
			return
		}
	}
}
