package interests

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestInterestCursorValidationAndVisibility(t *testing.T) {
	scope := "own:owner"
	cursor := nextCursor(scope, time.Now().UTC(), "ticketmaster:CaseSensitive")
	q, err := parseQuery(url.Values{"limit": {"1"}, "cursor": {*cursor}}, scope, false)
	if err != nil || q.Limit != 1 || q.Cursor.ID != "ticketmaster:CaseSensitive" {
		t.Fatal("valid keyset rejected", err)
	}
	for _, values := range []url.Values{
		{"cursor": {*cursor}}, {"cursor": {"bad"}}, {"cursor": {strings.Repeat("a", 513)}},
		{"limit": {"0"}}, {"limit": {"101"}}, {"limit": {"1", "2"}}, {"visibility": {"public"}},
	} {
		if _, err := parseQuery(values, "public:owner", false); err == nil {
			t.Fatal("invalid/cross-audience query accepted", values)
		}
	}
	for _, value := range []string{"", "friends", "Public"} {
		if Visibility(value) == nil {
			t.Fatal("unsupported visibility", value)
		}
	}
}
