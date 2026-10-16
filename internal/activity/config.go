package activity

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Enabled                     bool
	MaxStreams, StartsPerMinute int
}

func ConfigFromEnv(databaseEnabled, accountsEnabled bool) (Config, error) {
	c := Config{Enabled: databaseEnabled && accountsEnabled, MaxStreams: 256, StartsPerMinute: 60}
	if raw := os.Getenv("LIVE_ACTIVITY_ENABLED"); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return c, fmt.Errorf("LIVE_ACTIVITY_ENABLED must be true or false")
		}
		c.Enabled = v
	}
	if c.Enabled && (!databaseEnabled || !accountsEnabled) {
		return c, fmt.Errorf("live activity requires MariaDB and accounts")
	}
	for _, setting := range []struct {
		name   string
		target *int
		max    int
	}{{"LIVE_ACTIVITY_MAX_STREAMS", &c.MaxStreams, 4096}, {"LIVE_ACTIVITY_STARTS_PER_MINUTE", &c.StartsPerMinute, 600}} {
		if raw := os.Getenv(setting.name); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil || v < 1 || v > setting.max {
				return c, fmt.Errorf("%s must be between 1 and %d", setting.name, setting.max)
			}
			*setting.target = v
		}
	}
	return c, nil
}
