package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type AlarmRule struct {
	Tag       string  `yaml:"tag"`
	Operator  string  `yaml:"operator"`
	Threshold float64 `yaml:"threshold"`
}

type Config struct {
	OPCUA struct {
		Endpoint     string   `yaml:"endpoint"`
		Tags         []string `yaml:"tags"`
		PollInterval int      `yaml:"poll_interval_seconds"`
	} `yaml:"opcua"`

	MQTT struct {
		Broker string `yaml:"broker"`
		Topic  string `yaml:"topic"`
	} `yaml:"mqtt"`

	Buffer struct {
		Path string `yaml:"path"`
	} `yaml:"buffer"`

	Sink struct {
		DSN string `yaml:"dsn"`
	} `yaml:"sink"`

	Alarms []AlarmRule `yaml:"alarms"`

	Rollup struct {
		WindowSeconds int `yaml:"window_seconds"`
	} `yaml:"rollup"`

	ReplayInterval int `yaml:"replay_interval_seconds"`
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, err
	}

	applyEnvOverrides(cfg)
	return cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("OPCUA_ENDPOINT"); v != "" {
		cfg.OPCUA.Endpoint = v
	}
	if v := os.Getenv("MQTT_BROKER"); v != "" {
		cfg.MQTT.Broker = v
	}
	if v := os.Getenv("SINK_DSN"); v != "" {
		cfg.Sink.DSN = v
	}
	if v := os.Getenv("BUFFER_PATH"); v != "" {
		cfg.Buffer.Path = v
	}
}
