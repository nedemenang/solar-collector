package opcua

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
	"github.com/google/uuid"

	"solar-collector/internal/model"
)

// uaClient is the subset of *opcua.Client that Poller relies on. It exists
// so tests can substitute a fake instead of dialing a real OPC UA server.
type uaClient interface {
	Connect(ctx context.Context) error
	Close(ctx context.Context) error
	Read(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error)
}

type Poller struct {
	endpoint string
	tags     []string
	interval time.Duration

	// newClient constructs the OPC UA client used each connection attempt.
	// Defaulted to a real dialer in NewPoller; overridden in tests.
	newClient func(endpoint string) (uaClient, error)

	// reconnectDelay is how long Run waits before retrying after a
	// connection is lost. Defaulted in NewPoller; overridden in tests so
	// they don't have to sleep through a real 5s backoff.
	reconnectDelay time.Duration
}

func NewPoller(endpoint string, tags []string, interval time.Duration) *Poller {
	return &Poller{
		endpoint:       endpoint,
		tags:           tags,
		interval:       interval,
		newClient:      newRealClient,
		reconnectDelay: 5 * time.Second,
	}
}

// newRealClient dials a real OPC UA server via opcua.NewClient, wrapping the
// result as a uaClient. If opcua.NewClient fails, it returns a nil
// uaClient explicitly rather than letting a typed-nil *opcua.Client leak
// through as a non-nil interface.
func newRealClient(endpoint string) (uaClient, error) {
	c, err := opcua.NewClient(endpoint)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// Run polls every configured tag on a fixed interval and pushes each
// reading onto out. It reconnects on failure instead of giving up, since a
// site's OPC UA server may itself bounce independently of the WAN link.
func (p *Poller) Run(ctx context.Context, out chan<- model.Reading) error {
	for {
		err := p.pollLoop(ctx, out)
		if ctx.Err() != nil {
			return nil
		}
		slog.Warn("opcua connection lost, reconnecting", "error", err)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(p.reconnectDelay):
		}
	}
}

func (p *Poller) pollLoop(ctx context.Context, out chan<- model.Reading) error {
	c, err := p.newClient(p.endpoint)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}
	if err := c.Connect(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer c.Close(ctx)

	nodeIDs := make([]*ua.NodeID, len(p.tags))
	for i, tag := range p.tags {
		// ns=1 is node-opcua's default "own" namespace (0 is the
		// standard OPC UA namespace) — see simulators/opcua-server.
		id, err := ua.ParseNodeID(fmt.Sprintf("ns=1;s=%s", tag))
		if err != nil {
			return fmt.Errorf("parse node id for %s: %w", tag, err)
		}
		nodeIDs[i] = id
	}

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := p.pollOnce(ctx, c, nodeIDs, out); err != nil {
				return err
			}
		}
	}
}

func (p *Poller) pollOnce(ctx context.Context, c uaClient, nodeIDs []*ua.NodeID, out chan<- model.Reading) error {
	req := &ua.ReadRequest{
		MaxAge:             2000,
		TimestampsToReturn: ua.TimestampsToReturnBoth,
	}
	for _, id := range nodeIDs {
		req.NodesToRead = append(req.NodesToRead, &ua.ReadValueID{NodeID: id})
	}

	resp, err := c.Read(ctx, req)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	now := time.Now().UTC()
	for i, res := range resp.Results {
		if res.Status != ua.StatusOK || res.Value == nil {
			continue
		}
		value, ok := toFloat64(res.Value.Value())
		if !ok {
			continue
		}

		select {
		case out <- model.Reading{
			ID:        uuid.NewString(),
			Source:    "opcua",
			Tag:       p.tags[i],
			Value:     value,
			Timestamp: now,
		}:
		case <-ctx.Done():
			return nil
		}
	}
	return nil
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
