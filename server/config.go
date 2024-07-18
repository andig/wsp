package server

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config configures an Server
type Config struct {
	// Host        string
	// Port        string
	Timeout     time.Duration
	IdleTimeout time.Duration
	SecretKey   string
}

// // GetAddr returns the address to specify a HTTP server address
// func (c Config) GetAddr() string {
// 	return net.JoinHostPort(c.Host, c.Port)
// }

// NewConfig creates a new ProxyConfig
func NewConfig() *Config {
	return &Config{
		// Host:        "localhost",
		// Port:        "8080",
		Timeout:     time.Second,
		IdleTimeout: time.Minute,
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
