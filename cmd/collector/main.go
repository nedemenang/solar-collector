package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	mqttAdapter "solar-collector/internal/adapters/mqtt"
	opcuaAdapter "solar-collector/internal/adapters/opcua"
	"solar-collector/internal/alarm"
	"solar-collector/internal/config"
	"solar-collector/internal/ingest"
	"solar-collector/internal/model"
	"solar-collector/internal/rollup"
	"solar-collector/internal/sink"
)

func main() {
	configPath := flag.String("config", "config/config.yaml", "path to config file")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := ingest.Open(cfg.Buffer.Path)
	if err != nil {
		slog.Error("failed to open local buffer", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	pg, err := sink.NewPostgres(ctx, cfg.Sink.DSN)
	if err != nil {
		slog.Error("invalid sink configuration", "error", err)
		os.Exit(1)
	}
	defer pg.Close()

	var rules []alarm.Rule
	for _, a := range cfg.Alarms {
		rules = append(rules, alarm.Rule{Tag: a.Tag, Operator: a.Operator, Threshold: a.Threshold})
	}
	alarmEngine := alarm.NewEngine(rules)
	rollupEngine := rollup.NewAggregator(time.Duration(cfg.Rollup.WindowSeconds) * time.Second)

	readings := make(chan model.Reading, 256)

	go func() {
		poller := opcuaAdapter.NewPoller(cfg.OPCUA.Endpoint, cfg.OPCUA.Tags, time.Duration(cfg.OPCUA.PollInterval)*time.Second)
		if err := poller.Run(ctx, readings); err != nil {
			slog.Error("opcua poller stopped", "error", err)
		}
	}()

	go func() {
		sub := mqttAdapter.NewSubscriber(cfg.MQTT.Broker, cfg.MQTT.Topic)
		if err := sub.Run(ctx, readings); err != nil {
			slog.Error("mqtt subscriber stopped", "error", err)
		}
	}()

	replayer := ingest.NewReplayer(store, pg, time.Duration(cfg.ReplayInterval)*time.Second)
	go replayer.Run(ctx)

	slog.Info("collector running", "opcua_endpoint", cfg.OPCUA.Endpoint, "mqtt_broker", cfg.MQTT.Broker)

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down")
			return
		case r := <-readings:
			if err := store.Put(ctx, r); err != nil {
				slog.Error("failed to buffer reading", "error", err)
				continue
			}

			if a := alarmEngine.Evaluate(r); a != nil {
				if a.ClearedAt != nil {
					slog.Info("alarm cleared", "tag", a.Tag, "value", a.Value)
				} else {
					slog.Warn("active alarm", "tag", a.Tag, "value", a.Value)
				}
				if err := store.PutAlarm(ctx, *a); err != nil {
					slog.Error("failed to buffer alarm", "error", err)
				}
			}

			if roll := rollupEngine.Add(r); roll != nil {
				slog.Info("rollup complete", "tag", roll.Tag, "min", roll.Min, "max", roll.Max, "avg", roll.Avg, "count", roll.Count)
				if err := store.PutRollup(ctx, *roll); err != nil {
					slog.Error("failed to buffer rollup", "error", err)
				}
			}
		}
	}
}
