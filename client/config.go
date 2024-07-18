package client

import (
	"os"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// Config configures an Proxy
type Config struct {
	ID           string
	Targets      []string
	PoolIdleSize int
	PoolMaxSize  int
	SecretKey    string
}

// NewConfig creates a new ProxyConfig
func NewConfig() *Config {
	return &Config{
		ID:           uuid.NewString(),
		Targets:      []string{"ws://127.0.0.1:8080/register"},
		PoolIdleSize: 10,
		PoolMaxSize:  100,
	}
}

// LoadConfiguration loads configuration from a YAML file
func LoadConfiguration(path string) (config *Config, err error) {
	config = NewConfig()

	bytes, err := os.ReadFile(path)
	if err != nil {
		return
	}

	err = yaml.Unmarshal(bytes, config)
	if err != nil {
		return
	}

	return
}
