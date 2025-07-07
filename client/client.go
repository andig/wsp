package client

import (
	"context"
	"net/http"
)

// Client connects to one or more Server using HTTP websockets.
// The Server can then send HTTP requests to execute.
type Client struct {
	Config *Config

	client *http.Client
	pools  map[string]*Pool
}

// NewClient creates a new Client.
func NewClient(config *Config) *Client {
	c := &Client{
		Config: config,
		client: &http.Client{},
		pools:  make(map[string]*Pool),
	}
	return c
}

// Start the Proxy
func (c *Client) Start(ctx context.Context) {
	for _, target := range c.Config.Targets {
		pool := NewPool(c, target, c.Config.SecretKey)
		c.pools[target] = pool
		go pool.Start(ctx)
	}
}

// Shutdown the Proxy
func (c *Client) Shutdown() {
	for _, pool := range c.pools {
		pool.Shutdown()
	}
}
