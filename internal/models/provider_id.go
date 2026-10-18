package models

import "regexp"

var providerID = regexp.MustCompile(`^ticketmaster:[A-Za-z0-9_-]{1,128}$`)

// ValidProviderID preserves the provider's case-sensitive resource identity.
func ValidProviderID(id string) bool { return providerID.MatchString(id) }
