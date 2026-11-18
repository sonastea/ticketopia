package appmcp

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sonastea/ticketopia/internal/models"
)

// Mistyped, missing and unknown arguments are refused in the API's
// "field: message" wording, naming every problem, before any tool runs.
func TestArgumentMistakesUseAPIWording(t *testing.T) {
	f := &fakeEvents{detail: map[string]models.EventDetail{"ticketmaster:known": {Item: models.Event{ID: "ticketmaster:known", Name: "Known"}}}}
	s := session(t, f)
	for _, tc := range []struct {
		name, tool string
		args       any
		want       string
	}{
		{"string limit", "find_events", map[string]any{"limit": "abc"}, "limit: use an integer from 1 to 100"},
		{"fractional limit", "find_events", map[string]any{"limit": 2.5}, "limit: use an integer from 1 to 100"},
		{"mistyped text", "find_events", map[string]any{"city": 42}, "city: use a string (city name, for example Chicago)"},
		{"missing required", "event_details", map[string]any{}, "event_id: required (event ID from find_events)"},
		{"null required", "event_details", map[string]any{"event_id": nil}, "event_id: use a string (event ID from find_events)"},
		{"unknown argument", "event_details", map[string]any{"event_id": "ticketmaster:known", "id": "x"},
			"id: not an argument of event_details; use event_id"},
		{"no arguments taken", "list_categories", map[string]any{"city": "Chicago"}, "city: list_categories takes no arguments"},
		{"unknown arguments listed once", "event_details", map[string]any{"event_id": "ticketmaster:known", "id": "x", "eventId": "y"},
			"eventId, id: not arguments of event_details; use event_id"},
		{"every problem, in name order", "event_details", map[string]any{"discussion_id": "x"},
			"discussion_id: not an argument of event_details; use event_id; event_id: required (event ID from find_events)"},
		{"not an object", "event_details", []string{"ticketmaster:known"}, "arguments: send the arguments as a JSON object of named values"},
	} {
		result, err := s.CallTool(t.Context(), callParams(tc.tool, tc.args))
		if err != nil {
			t.Fatal(tc.name, err)
		}
		if !result.IsError || text(result) != tc.want || result.StructuredContent != nil {
			t.Errorf("%s: %q, want %q", tc.name, text(result), tc.want)
		}
	}
	if f.query.Limit != 0 {
		t.Fatal("a refused call reached the event service")
	}
	// Omitting the arguments object is valid for a tool with no arguments.
	if result, _ := call(t, s, "list_categories", nil); result.IsError {
		t.Fatal(text(result))
	}
}

func callParams(name string, args any) *mcp.CallToolParams {
	return &mcp.CallToolParams{Name: name, Arguments: args}
}
