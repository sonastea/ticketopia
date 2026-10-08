package home

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sonastea/ticketopia/internal/models"
)

func TestFollowRowLocation(t *testing.T) {
	for _, tc := range []struct {
		name, kind, want string
		location         *models.Place
	}{
		{name: "missing venue location", kind: "venue", want: "Location not supplied"},
		{name: "empty venue location", kind: "venue", location: &models.Place{}, want: "Location not supplied"},
		{name: "partial venue location", kind: "venue", location: &models.Place{City: "Boston"}, want: "Boston"},
		{name: "full venue location", kind: "venue", location: &models.Place{Address: "1 Main Street", City: "Boston", State: "MA", Country: "US"}, want: "1 Main Street, Boston, MA, US"},
		{name: "artist has no location guidance", kind: "artist"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := models.FollowTarget{Kind: tc.kind, ID: "ticketmaster:Location", Name: "Location fixture", Location: tc.location}
			var out bytes.Buffer
			if err := FollowRow(target, false, FollowsPage{ReturnURL: "/follows"}).Render(t.Context(), &out); err != nil {
				t.Fatal(err)
			}
			if tc.want != "" && !strings.Contains(out.String(), ">"+tc.want+"</p>") {
				t.Fatalf("missing location guidance %q: %s", tc.want, out.String())
			}
			if tc.kind == "artist" && strings.Contains(out.String(), "Location not supplied") {
				t.Fatal("artist row invented location guidance")
			}
		})
	}
}
