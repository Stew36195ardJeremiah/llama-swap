package proxy

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds the top-level configuration for llama-swap.
type Config struct {
	// LogLevel controls verbosity: "debug", "info", "warn", "error"
	LogLevel string `yaml:"log_level" json:"log_level"`

	// HealthCheckTimeout is how long to wait for a model process to become
	// healthy before considering it failed.
	HealthCheckTimeout Duration `yaml:"health_check_timeout" json:"health_check_timeout"`

	// Models is a map of model name -> model configuration.
	Models map[string]ModelConfig `yaml:"models" json:"models"`

	// Groups allows aliasing multiple model names to a single endpoint.
	Groups map[string]GroupConfig `yaml:"groups" json:"groups"`
}

// ModelConfig describes a single model backend process.
type ModelConfig struct {
	// Cmd is the command (and arguments) used to start the model server.
	Cmd string `yaml:"cmd" json:"cmd"`

	// Proxy is the upstream address to forward requests to, e.g. "http://127.0.0.1:8080".
	Proxy string `yaml:"proxy" json:"proxy"`

	// Aliases are additional names that resolve to this model.
	Aliases []string `yaml:"aliases" json:"aliases"`

	// Env holds extra environment variables to pass to the model process.
	Env []string `yaml:"env" json:"env"`

	// CheckEndpoint overrides the URL path used for health checks (default: "/health").
	CheckEndpoint string `yaml:"check_endpoint" json:"check_endpoint"`

	// UnloadAfter specifies an idle duration after which the model process is
	// stopped to free resources. Zero means never unload.
	// Personal note: I typically set this to "15m" on my dev machine to avoid
	// leaving large models loaded when I step away.
	UnloadAfter Duration `yaml:"unload_after" json:"unload_after"`

	// UseGPU indicates whether the model should be pinned to a GPU slot.
	UseGPU bool `yaml:"use_gpu" json:"use_gpu"`

	// TTY allocates a pseudo-terminal for the model process when true.
	TTY bool `yaml:"tty" json:"tty"`
}

// GroupConfig maps a group name to one or more model names, enabling
// round-robin or fallback routing across multiple model backends.
type GroupConfig struct {
	// Members lists the model names that belong to this group.
	Members []string `yaml:"members" json:"members"`

	// Swap controls whether only one member is kept running at a time.
	Swap bool `yaml:"swap" json:"swap"`
}

// Duration is a yaml-serialisable wrapper around time.Duration.
type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = parsed
	return nil
}

func (d Duration) MarshalYAML() (interface{}, error) {
	return d.Duration.String(), nil
}

// LoadConfig reads and parses a YAML configuration file from the given path.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data,
