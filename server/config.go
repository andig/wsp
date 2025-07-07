package server

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config configures an Server
type Config struct {
	Timeout     time.Duration
	IdleTimeout time.Duration
	SecretKey   string
}

// NewConfig creates a new ProxyConfig
func NewConfig() *Config {
	return &Config{
		Timeout:     time.Second,
		IdleTimeout: time.Minute,
	}
}

// LoadConfiguration loads configuration from a YAML file
func LoadConfiguration(path string) (*Config, error) {
	config := NewConfig()

	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(bytes, config); err != nil {
		return nil, err
	}

	return config, nil
}
