package useractivity

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"testing"

	"github.com/sonastea/ticketopia/internal/accounts"
)

func cursor(raw string) url.Values {
	return url.Values{"cursor": {base64.RawURLEncoding.EncodeToString([]byte(raw))}}
}

func TestCursorRejectsForeignShapes(t *testing.T) {
	scope := "user:" + accounts.ID()
	for _, raw := range []string{
		`{"scope":"` + scope + `","at":"2026-11-01T12:00:00Z","rank":7,"id":"ticketmaster:A"}`,
		`{"scope":"` + scope + `","at":"2026-11-01T12:00:00Z","rank":1,"id":"ticketmaster:A"}`,
		`{"scope":"` + scope + `","at":"2026-11-01T12:00:00Z","rank":2,"id":"not-an-event"}`,
		`{"scope":"` + scope + `","rank":0,"id":"ticketmaster:A"}`,
		`{"scope":"user:other","at":"2026-11-01T12:00:00Z","rank":0,"id":"ticketmaster:A"}`,
	} {
		if _, err := ParseQuery(cursor(raw), scope); err == nil {
			t.Fatal("accepted cursor", raw)
		}
	}
	q, err := ParseQuery(cursor(`{"scope":"`+scope+`","at":"2026-11-01T12:00:00Z","rank":0,"id":"ticketmaster:A"}`), scope)
	if err != nil || q.Cursor == nil || q.Limit != 20 {
		t.Fatal("rejected a valid cursor", err)
	}
}

func TestUnwiredRepositoryIsUnavailableAndInvalidOwnerMissing(t *testing.T) {
	if _, err := New(nil).List(context.Background(), accounts.ID(), nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing repository reported", err)
	}
	if _, err := New(nil).List(context.Background(), "bad id", nil); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatal("invalid owner reported", err)
	}
}
