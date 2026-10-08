package discovery

import (
	"fmt"
	"os"
	"strconv"
)

func ConfigFromEnv() (Config, error) {
	c := Config{APIKey: os.Getenv("TICKETMASTER_KEY"), DailyBudget: 4500}
	if value := os.Getenv("TICKETMASTER_DAILY_BUDGET"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 2147483647 {
			return c, fmt.Errorf("TICKETMASTER_DAILY_BUDGET must be a positive 32-bit integer")
		}
		c.DailyBudget = parsed
	}
	return c, nil
}
