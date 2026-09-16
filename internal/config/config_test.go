package config

import (
	"os"
	"path/filepath"
	"testing"
)

const validYAML = `
opcua:
  endpoint: "opc.tcp://localhost:4840"
  tags:
    - "temperature"
    - "pressure"
  poll_interval_seconds: 5

mqtt:
  broker: "tcp://localhost:1883"
  topic: "spBv1.0/site/+/edge"

buffer:
  path: "/var/lib/collector/buffer.db"

sink:
  dsn: "postgres://user:pass@localhost:5432/collector"

alarms:
  - tag: "temperature"
    operator: ">"
    threshold: 100
  - tag: "pressure"
    operator: "<"
    threshold: 10

rollup:
  window_seconds: 60

replay_interval_seconds: 30
`

const malformedYAML = `
opcua: [this is not
  valid: yaml: at all
`

func writeFixture(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("failed to write fixture %s: %v", path, err)
	}
	return path
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		path    string // if set, used instead of writing yaml to a temp file
		wantErr bool
		check   func(t *testing.T, cfg *Config)
	}{
		{
			name: "valid full config loads all fields",
			yaml: validYAML,
			check: func(t *testing.T, cfg *Config) {
				if cfg.OPCUA.Endpoint != "opc.tcp://localhost:4840" {
					t.Errorf("OPCUA.Endpoint = %q", cfg.OPCUA.Endpoint)
				}
				if len(cfg.OPCUA.Tags) != 2 || cfg.OPCUA.Tags[0] != "temperature" || cfg.OPCUA.Tags[1] != "pressure" {
					t.Errorf("OPCUA.Tags = %v", cfg.OPCUA.Tags)
				}
				if cfg.OPCUA.PollInterval != 5 {
					t.Errorf("OPCUA.PollInterval = %d, want 5", cfg.OPCUA.PollInterval)
				}
				if cfg.MQTT.Broker != "tcp://localhost:1883" {
					t.Errorf("MQTT.Broker = %q", cfg.MQTT.Broker)
				}
				if cfg.MQTT.Topic != "spBv1.0/site/+/edge" {
					t.Errorf("MQTT.Topic = %q", cfg.MQTT.Topic)
				}
				if cfg.Buffer.Path != "/var/lib/collector/buffer.db" {
					t.Errorf("Buffer.Path = %q", cfg.Buffer.Path)
				}
				if cfg.Sink.DSN != "postgres://user:pass@localhost:5432/collector" {
					t.Errorf("Sink.DSN = %q", cfg.Sink.DSN)
				}
				if len(cfg.Alarms) != 2 {
					t.Fatalf("len(Alarms) = %d, want 2", len(cfg.Alarms))
				}
				if cfg.Alarms[0] != (AlarmRule{Tag: "temperature", Operator: ">", Threshold: 100}) {
					t.Errorf("Alarms[0] = %+v", cfg.Alarms[0])
				}
				if cfg.Alarms[1] != (AlarmRule{Tag: "pressure", Operator: "<", Threshold: 10}) {
					t.Errorf("Alarms[1] = %+v", cfg.Alarms[1])
				}
				if cfg.Rollup.WindowSeconds != 60 {
					t.Errorf("Rollup.WindowSeconds = %d, want 60", cfg.Rollup.WindowSeconds)
				}
				if cfg.ReplayInterval != 30 {
					t.Errorf("ReplayInterval = %d, want 30", cfg.ReplayInterval)
				}
			},
		},
		{
			name:    "nonexistent path returns an error",
			path:    "/nonexistent/path/does-not-exist.yaml",
			wantErr: true,
		},
		{
			name:    "malformed YAML content returns an error",
			yaml:    malformedYAML,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.path
			if path == "" {
				dir := t.TempDir()
				path = writeFixture(t, dir, "config.yaml", tt.yaml)
			}

			cfg, err := Load(path)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Load(%q) error = nil, want error", path)
				}
				return
			}

			if err != nil {
				t.Fatalf("Load(%q) unexpected error: %v", path, err)
			}
			if cfg == nil {
				t.Fatal("Load() returned nil config with nil error")
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	tests := []struct {
		name     string
		envVar   string
		envValue string
		check    func(cfg *Config) string // returns the field's current value
		yamlVal  string
	}{
		{
			name:     "OPCUA_ENDPOINT overrides yaml value",
			envVar:   "OPCUA_ENDPOINT",
			envValue: "opc.tcp://override:4840",
			yamlVal:  "opc.tcp://localhost:4840",
			check:    func(cfg *Config) string { return cfg.OPCUA.Endpoint },
		},
		{
			name:     "MQTT_BROKER overrides yaml value",
			envVar:   "MQTT_BROKER",
			envValue: "tcp://override:1883",
			yamlVal:  "tcp://localhost:1883",
			check:    func(cfg *Config) string { return cfg.MQTT.Broker },
		},
		{
			name:     "SINK_DSN overrides yaml value",
			envVar:   "SINK_DSN",
			envValue: "postgres://override@localhost/db",
			yamlVal:  "postgres://user:pass@localhost:5432/collector",
			check:    func(cfg *Config) string { return cfg.Sink.DSN },
		},
		{
			name:     "BUFFER_PATH overrides yaml value",
			envVar:   "BUFFER_PATH",
			envValue: "/override/buffer.db",
			yamlVal:  "/var/lib/collector/buffer.db",
			check:    func(cfg *Config) string { return cfg.Buffer.Path },
		},
	}

	// Not run in parallel: t.Setenv mutates shared process env, and
	// parallel subtests would race with each other's overrides.
	for _, tt := range tests {
		t.Run(tt.name+"/set", func(t *testing.T) {
			dir := t.TempDir()
			path := writeFixture(t, dir, "config.yaml", validYAML)

			t.Setenv(tt.envVar, tt.envValue)

			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			if got := tt.check(cfg); got != tt.envValue {
				t.Errorf("after setting %s, field = %q, want %q", tt.envVar, got, tt.envValue)
			}
		})

		t.Run(tt.name+"/unset", func(t *testing.T) {
			dir := t.TempDir()
			path := writeFixture(t, dir, "config.yaml", validYAML)

			// Explicitly ensure the var is unset for this subtest.
			t.Setenv(tt.envVar, "")

			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			if got := tt.check(cfg); got != tt.yamlVal {
				t.Errorf("with %s unset, field = %q, want yaml value %q", tt.envVar, got, tt.yamlVal)
			}
		})
	}
}
