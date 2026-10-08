package ticketopia

import (
	"strings"
	"testing"

	"github.com/sonastea/ticketopia/internal/accounts"
)

func TestModeratorCLIRequiresExplicitIdentityAndOperator(t *testing.T) {
	id := accounts.ID()
	for _, args := range [][]string{nil, {"bootstrap"}, {"grant", "--account-id", id}, {"grant", "--account-id", "someone@example.com", "--operator", "owner"}, {"revoke", "--account-id", id, "--operator", "owner", "extra"}, {"grant", "--account-id", id, "--operator", "owner\nother"}, {"grant", "--account-id", id, "--operator", strings.Repeat("x", 121)}} {
		if _, err := parseModeratorCommand(args); err == nil {
			t.Fatal("invalid command", args)
		}
	}
	for _, action := range []string{"grant", "revoke"} {
		c, err := parseModeratorCommand([]string{action, "--account-id", id, "--operator", " owner "})
		if err != nil || c.AccountID != id || c.Operator != "owner" || c.Grant != (action == "grant") {
			t.Fatal(c, err)
		}
	}
}
