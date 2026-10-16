package account

import "github.com/sonastea/ticketopia/internal/accounts"

func activityName(a accounts.Account, fallback string) string {
	if a.ActivityFirstName == nil || *a.ActivityFirstName == "" {
		return fallback
	}
	return *a.ActivityFirstName
}
