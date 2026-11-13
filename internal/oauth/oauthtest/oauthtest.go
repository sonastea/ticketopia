// Package oauthtest provides stand-ins for the OAuth service in tests and
// review fixtures. cmd/ticketopia never uses it: production stores codes and
// grants in MariaDB and fetches real client metadata documents.
package oauthtest

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/sonastea/ticketopia/internal/oauth"
)

// Repository keeps codes and grants in memory with the MariaDB repository's
// single-use and rotation rules.
type Repository struct {
	mu     sync.Mutex
	Codes  map[[32]byte]oauth.Code
	Grants map[string]oauth.Grant
	// Used maps rotated refresh tokens to their grant and rotation time.
	Used map[[32]byte]used
	// Fail makes every call return this error.
	Fail error
}

type used struct {
	grant string
	at    time.Time
}

func NewRepository() *Repository {
	return &Repository{Codes: map[[32]byte]oauth.Code{}, Grants: map[string]oauth.Grant{}, Used: map[[32]byte]used{}}
}

func (r *Repository) PutCode(_ context.Context, c oauth.Code) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Fail != nil {
		return r.Fail
	}
	r.Codes[c.Hash] = c
	return nil
}

func (r *Repository) Redeem(_ context.Context, hash [32]byte, now time.Time, grant func(oauth.Code) (oauth.Grant, error)) (oauth.Grant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Fail != nil {
		return oauth.Grant{}, r.Fail
	}
	c, ok := r.Codes[hash]
	delete(r.Codes, hash)
	if !ok || !now.Before(c.ExpiresAt) {
		return oauth.Grant{}, oauth.ErrInvalidGrant
	}
	g, err := grant(c)
	if err != nil {
		return oauth.Grant{}, err
	}
	r.add(g, now)
	return g, nil
}

// CreateGrant stores a grant directly, for fixtures.
func (r *Repository) CreateGrant(g oauth.Grant, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.add(g, now)
}

func (r *Repository) add(g oauth.Grant, now time.Time) {
	var owned []oauth.Grant
	for id, existing := range r.Grants {
		if existing.AccountID != g.AccountID {
			continue
		}
		if !now.Before(existing.ExpiresAt) {
			delete(r.Grants, id)
			continue
		}
		owned = append(owned, existing)
	}
	slices.SortFunc(owned, func(a, b oauth.Grant) int { return a.CreatedAt.Compare(b.CreatedAt) })
	for len(owned) >= oauth.MaxGrants {
		delete(r.Grants, owned[0].ID)
		owned = owned[1:]
	}
	r.Grants[g.ID] = g
}

func (r *Repository) Refresh(_ context.Context, old [32]byte, clientID string, scopes []string, next oauth.Grant, now time.Time) (oauth.Grant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Fail != nil {
		return oauth.Grant{}, r.Fail
	}
	for id, g := range r.Grants {
		if g.RefreshHash != old || old == ([32]byte{}) {
			continue
		}
		if g.ClientID != clientID || !now.Before(g.ExpiresAt) {
			return oauth.Grant{}, oauth.ErrInvalidGrant
		}
		if !oauth.Subset(scopes, g.Scopes) {
			return oauth.Grant{}, oauth.ErrInvalidScope
		}
		r.Used[old] = used{id, now}
		g.AccessHash, g.AccessUntil, g.RefreshHash, g.ExpiresAt = next.AccessHash, next.AccessUntil, next.RefreshHash, next.ExpiresAt
		r.Grants[id] = g
		return g, nil
	}
	if u, ok := r.Used[old]; ok {
		delete(r.Grants, u.grant)
	}
	return oauth.Grant{}, oauth.ErrInvalidGrant
}

func (r *Repository) Authenticate(_ context.Context, access [32]byte, now time.Time) (oauth.Grant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Fail != nil {
		return oauth.Grant{}, r.Fail
	}
	for _, g := range r.Grants {
		if g.AccessHash == access && now.Before(g.AccessUntil) && now.Before(g.ExpiresAt) {
			return g, nil
		}
	}
	return oauth.Grant{}, oauth.ErrUnauthenticated
}

func (r *Repository) ListGrants(_ context.Context, account string, now time.Time) ([]oauth.Grant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Fail != nil {
		return nil, r.Fail
	}
	result := []oauth.Grant{}
	for _, g := range r.Grants {
		if g.AccountID == account && now.Before(g.ExpiresAt) {
			result = append(result, g)
		}
	}
	slices.SortFunc(result, func(a, b oauth.Grant) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return result, nil
}

func (r *Repository) RevokeGrant(_ context.Context, account, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Fail != nil {
		return r.Fail
	}
	if g, ok := r.Grants[id]; ok && g.AccountID == account {
		delete(r.Grants, id)
	}
	return nil
}

func (r *Repository) RevokeToken(_ context.Context, hash [32]byte, refresh bool, clientID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Fail != nil {
		return r.Fail
	}
	for id, g := range r.Grants {
		current := g.AccessHash
		if refresh {
			current = g.RefreshHash
		}
		if current == hash && hash != ([32]byte{}) && g.ClientID == clientID {
			delete(r.Grants, id)
		}
	}
	return nil
}

func (r *Repository) RevokeAll(_ context.Context, account string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Fail != nil {
		return r.Fail
	}
	for id, g := range r.Grants {
		if g.AccountID == account {
			delete(r.Grants, id)
		}
	}
	for hash, c := range r.Codes {
		if c.AccountID == account {
			delete(r.Codes, hash)
		}
	}
	return nil
}

// Clients resolves fixed clients without fetching documents.
type Clients map[string]oauth.Client

func (c Clients) Client(_ context.Context, id string) (oauth.Client, error) {
	client, ok := c[id]
	if !ok {
		return oauth.Client{}, &oauth.ClientError{Message: "unknown test client"}
	}
	return client, nil
}
