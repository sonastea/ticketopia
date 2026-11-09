package appmcp

import (
	"fmt"
	"os"
	"strconv"
)

// Config enables the /mcp endpoint. It is off unless explicitly enabled.
type Config struct {
	Enabled bool
	// RatePerMinute bounds tool requests per client IP on each replica.
	RatePerMinute int
}

func ConfigFromEnv() (Config, error) {
	c := Config{RatePerMinute: 60}
	if raw := os.Getenv("MCP_ENABLED"); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return c, fmt.Errorf("MCP_ENABLED must be true or false")
		}
		c.Enabled = v
	}
	if raw := os.Getenv("MCP_RATE_PER_MINUTE"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > 6000 {
			return c, fmt.Errorf("MCP_RATE_PER_MINUTE must be between 1 and 6000")
		}
		c.RatePerMinute = v
	}
	return c, nil
}
