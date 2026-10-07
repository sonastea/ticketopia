package account

import (
	"bytes"
	"net/url"
	"strings"
	"testing"
)

func TestSignInKeepsBrandedCTALinkAndReturnContext(t *testing.T) {
	returnTo := "/?city=Berlin&selected_event=ticketmaster:x&section=discussion"
	var out bytes.Buffer
	if err := SignIn(true, returnTo, "Sign-in was cancelled.").Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{
		`href="/auth/google/start?return_to=` + url.QueryEscape(returnTo) + `"`,
		`google-sign-in`,
		`<span>Sign in with Google</span>`,
		`<img src="/assets/google-g.png" width="20" height="20" alt="" aria-hidden="true"`,
		`Sign-in was cancelled.`,
		`Discover without signing in`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing sign-in contract %q", want)
		}
	}
}

func TestSignInHidesGoogleCTAWhenAccountsAreDisabled(t *testing.T) {
	var out bytes.Buffer
	if err := SignIn(false, "/me", "").Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, unwanted := range []string{"google-sign-in", "/auth/google/start", "/assets/google-g.png"} {
		if strings.Contains(html, unwanted) {
			t.Errorf("disabled accounts rendered %q", unwanted)
		}
	}
	if !strings.Contains(html, "Accounts aren't enabled") || !strings.Contains(html, "Discover without signing in") {
		t.Fatal("disabled sign-in must explain availability and retain discovery")
	}
}
