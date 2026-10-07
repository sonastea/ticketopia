package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type validationRepository struct {
	Repository
	profile     ProfilePatch
	preferences PreferencesPatch
}

func (r *validationRepository) UpdateProfile(_ context.Context, _ string, p ProfilePatch) (Account, error) {
	r.profile = p
	return Account{}, nil
}
func (r *validationRepository) UpdatePreferences(_ context.Context, _ string, p PreferencesPatch) (Account, error) {
	r.preferences = p
	return Account{}, nil
}
func ptr[T any](v T) *T { return &v }

func TestAccountValidationAndPatchSemantics(t *testing.T) {
	r := &validationRepository{}
	s := New(r, nil)
	for _, p := range []ProfilePatch{{}, {DisplayName: ptr(" ")}, {DisplayName: ptr(strings.Repeat("n", 81))}, {DisplayName: ptr("line\nbreak")}, {Bio: ptr(strings.Repeat("x", 501))}, {InterestVisibility: ptr("friends")}} {
		var invalid *ValidationError
		if _, err := s.UpdateProfile(t.Context(), "owner", p); !errors.As(err, &invalid) {
			t.Fatalf("accepted invalid profile: %+v %v", p, err)
		}
	}
	if _, err := s.UpdateProfile(t.Context(), "owner", ProfilePatch{DisplayName: ptr("  Concert pal  "), Bio: ptr(""), InterestVisibility: ptr("private")}); err != nil || *r.profile.DisplayName != "Concert pal" || *r.profile.Bio != "" {
		t.Fatal("profile normalization/clear", err)
	}
	if _, err := s.UpdateProfile(t.Context(), "owner", ProfilePatch{Bio: ptr("one\r\ntwo\rthree")}); err != nil || *r.profile.Bio != "one\ntwo\nthree" {
		t.Fatal("native textarea line endings", err)
	}
	for _, p := range []PreferencesPatch{{}, {Country: ptr("USA")}, {City: ptr("bad\x00city")}, {City: ptr(strings.Repeat("京", 41))}, {Timezone: ptr("Local")}, {Timezone: ptr("Mars/City")}, {CategoryIDs: ptr([]string{"bad/id"})}} {
		var invalid *ValidationError
		if _, err := s.UpdatePreferences(t.Context(), "owner", p); !errors.As(err, &invalid) {
			t.Fatalf("accepted invalid preferences: %+v %v", p, err)
		}
	}
	if _, err := s.UpdatePreferences(t.Context(), "owner", PreferencesPatch{City: ptr(" Berlin "), Country: ptr("de"), Timezone: ptr("Europe/Berlin"), CategoryIDs: ptr([]string{"sports", "music", "sports"}), NotificationsPaused: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	if *r.preferences.City != "Berlin" || *r.preferences.Country != "DE" || !reflect.DeepEqual(*r.preferences.CategoryIDs, []string{"music", "sports"}) || *r.preferences.NotificationsPaused {
		t.Fatal("preferences not normalized")
	}
	if _, err := s.UpdatePreferences(t.Context(), "owner", PreferencesPatch{CategoryIDs: ptr([]string{})}); err != nil || len(*r.preferences.CategoryIDs) != 0 || r.preferences.City != nil {
		t.Fatal("clear/omitted semantics", err)
	}
}
func TestPublicProfileHasOnlyAllowlistedFields(t *testing.T) {
	a := Account{ID: ID(), DisplayName: "Pal", Bio: "Hello", Email: "private@example.com", InterestVisibility: "private", Preferences: Preferences{City: "Secret city", CategoryIDs: []string{"private-interest"}}}
	data, err := json.Marshal(a.Public())
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err = json.Unmarshal(data, &fields); err != nil || len(fields) != 3 {
		t.Fatal("public projection is not allowlisted")
	}
	for _, secret := range []string{a.Email, a.Preferences.City, "private-interest", "interest_visibility", "preferences"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("private field leaked", secret)
		}
	}
}
func TestSecretsAndUnauthenticatedInputs(t *testing.T) {
	a, b := secret(), secret()
	if a == b || !validSecret(a) || len(a) != 43 || Hash(a) == Hash(b) || csrf(a) == a || csrf(a) == csrf(b) {
		t.Fatal("secrets lack separation")
	}
	s := New(nil, nil)
	for _, raw := range []string{"", "junk", a + "="} {
		if _, err := s.Authenticate(t.Context(), raw, "session"); !errors.Is(err, ErrUnauthenticated) {
			t.Fatal(err)
		}
	}
	if _, _, _, err := s.Complete(t.Context(), a, "wrong browser", "code"); !errors.Is(err, ErrFlow) {
		t.Fatal(err)
	}
}
