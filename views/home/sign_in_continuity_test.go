package home

import (
	"bytes"
	"html"
	"net/url"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func TestParticipationSignInResumesExplicitEventTask(t *testing.T) {
	id := "ticketmaster:resume"
	origin := "/?city=Berlin&category_id=music&cursor=next&limit=2"
	selected := SelectionURL(origin, id, "discussion")
	community := SelectionURL(origin, id, "community")
	for _, tc := range []struct {
		name   string
		view   templ.Component
		target string
		intent string
	}{
		{"row save selects event", SaveControl(id, "Resume show", SaveView{Enabled: true}, origin), SelectionURL(origin, id, "overview"), "save"},
		{"row save replaces other thread", SaveControl(id, "Resume show", SaveView{Enabled: true}, origin+"&selected_event=ticketmaster:other&section=discussion&selected_thread=other&reply_to=reply&discussion_cursor=old"), SelectionURL(origin, id, "overview"), "save"},
		{"event save preserves section", SaveControl(id, "Resume show", SaveView{Enabled: true}, selected), selected, "save"},
		{"interest preserves section", InterestControl(id, "Resume show", InterestView{Enabled: true}, selected), selected, ""},
		{"recommend opens composer", RecommendationPanel(id, RecommendationView{Enabled: true}, community), community + "&recommend=true", ""},
		{"recommend beside thread opens community composer", RecommendationPanel(id, RecommendationView{Enabled: true}, selected+"&selected_thread=thread&reply_to=reply&discussion_cursor=next"), community + "&recommend=true", ""},
		{"recommend from standalone thread keeps collection", RecommendationPanel(id, RecommendationView{Enabled: true}, "/events/"+id+"/discussions/thread?cursor=reply-page&limit=1&reply_to=reply&post_id=post&return_to="+url.QueryEscape(origin)), "/events/" + id + "?section=community&recommend=true&return_to=" + url.QueryEscape(origin), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := tc.view.Render(t.Context(), &out); err != nil {
				t.Fatal(err)
			}
			// Query ordering is immaterial; inspect the native link's destination.
			markup := out.String()
			prefix := `href="/auth/sign-in?`
			start := strings.Index(markup, prefix)
			if start < 0 {
				t.Fatal("missing native sign-in link", markup)
			}
			link, err := url.Parse(html.UnescapeString(strings.SplitN(markup[start+len(`href="`):], `"`, 2)[0]))
			if err != nil {
				t.Fatal(err)
			}
			// Only Save is completed by sign-in; other actions still need a choice.
			if got := link.Query().Get("intent"); got != tc.intent {
				t.Fatalf("intent = %q, want %q", got, tc.intent)
			}
			got := link.Query().Get("return_to")
			actual, _ := url.Parse(got)
			want, _ := url.Parse(tc.target)
			if actual.Path != want.Path || actual.Query().Encode() != want.Query().Encode() {
				t.Fatalf("lost task: %s, want %s", got, tc.target)
			}
		})
	}
}
