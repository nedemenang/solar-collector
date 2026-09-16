package mqtt

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/weekaung/sparkplugb-client/sparkplug"

	"solar-collector/internal/model"
)

type Subscriber struct {
	broker    string
	topic     string
	newClient func(*paho.ClientOptions) paho.Client
}

func NewSubscriber(broker, topic string) *Subscriber {
	return &Subscriber{broker: broker, topic: topic, newClient: paho.NewClient}
}

// Run subscribes to the Sparkplug B DDATA topic and decodes each protobuf
// payload into readings. paho's client reconnects on its own when the
// broker connection drops, so a network partition here just pauses the
// stream rather than crashing the adapter.
func (s *Subscriber) Run(ctx context.Context, out chan<- model.Reading) error {
	opts := paho.NewClientOptions().
		AddBroker(s.broker).
		SetClientID("solar-collector").
		SetAutoReconnect(true).
		SetConnectRetry(true)

	msgs := make(chan paho.Message, 64)
	opts.SetOnConnectHandler(func(c paho.Client) {
		slog.Info("mqtt connected", "broker", s.broker)
		token := c.Subscribe(s.topic, 1, func(_ paho.Client, m paho.Message) {
			msgs <- m
		})
		token.Wait()
		if err := token.Error(); err != nil {
			slog.Error("mqtt subscribe failed", "error", err)
		} else {
			slog.Info("mqtt subscribed", "topic", s.topic)
		}
	})
	opts.SetConnectionLostHandler(func(_ paho.Client, err error) {
		slog.Warn("mqtt connection lost, will auto-reconnect", "error", err)
	})

	client := s.newClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	defer client.Disconnect(250)

	for {
		select {
		case <-ctx.Done():
			return nil
		case m := <-msgs:
			s.handle(m.Payload(), out)
		}
	}
}

func (s *Subscriber) handle(raw []byte, out chan<- model.Reading) {
	payload := &sparkplug.Payload{}
	if err := payload.DecodePayload(raw); err != nil {
		slog.Warn("failed to decode sparkplug payload", "error", err)
		return
	}

	ts := payload.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	for _, m := range payload.Metrics {
		value, err := strconv.ParseFloat(m.Value, 64)
		if err != nil {
			continue
		}
		out <- model.Reading{
			ID:        uuid.NewString(),
			Source:    "mqtt",
			Tag:       m.Name,
			Value:     value,
			Timestamp: ts,
		}
	}
}
