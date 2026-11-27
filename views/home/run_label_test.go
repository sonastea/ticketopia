package home

import (
	"testing"

	"github.com/sonastea/ticketopia/internal/models"
)

// A folded row keeps the date and time of the listing it opens; its run note
// says what else there is.
func TestRunLabel(t *testing.T) {
	clock := func(s string) *string { return &s }
	for _, tt := range []struct {
		run  *models.Run
		want string
	}{
		{nil, ""},
		{&models.Run{Count: 19, Days: 1, LastDate: "2026-11-27", FirstTime: clock("18:00:00"), LastTime: clock("21:30:00")}, "Also 18 more times that day, until 9:30 PM"},
		{&models.Run{Count: 2, Days: 1, LastDate: "2026-11-27"}, "Also 1 more time that day"},
		{&models.Run{Count: 12, Days: 12, LastDate: "2026-12-27"}, "Also on 11 more dates, through Sun, Dec 27"},
		{&models.Run{Count: 412, Days: 27, LastDate: "2026-12-26", Changed: 3}, "412 times over 27 days, through Sat, Dec 26. Some dates changed"},
	} {
		if got := runLabel(tt.run); got != tt.want {
			t.Errorf("runLabel(%+v) = %q, want %q", tt.run, got, tt.want)
		}
	}
}
