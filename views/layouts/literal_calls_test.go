package layouts

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// A templ component call placed after bare text on the same line is emitted
// as literal text ("· @layouts.Stamp(...)") instead of being called. Guard the
// generated views against that silent failure.
func TestGeneratedViewsCallComponentsInsteadOfPrintingThem(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "*", "*_templ.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no generated views found: %v", err)
	}
	literal := regexp.MustCompile(`WriteString\([^)]*"[^"]*@(layouts|home|account|button|input|label|alert)\.[A-Z]`)
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if match := literal.Find(body); match != nil {
			t.Errorf("%s prints a component call as text: %s", file, match)
		}
	}
}

// A `{ "text" }` directly after a call becomes that component's children; a
// component that renders none (Stamp) silently drops the text.
func TestStampCallsHaveNoSwallowedChildren(t *testing.T) {
	sources, err := filepath.Glob(filepath.Join("..", "*", "*.templ"))
	if err != nil || len(sources) == 0 {
		t.Fatalf("no templ sources found: %v", err)
	}
	// Arguments may hold one level of nested calls, such as layouts.Now(ctx).
	swallowed := regexp.MustCompile(`@layouts\.Stamp(?:Date)?\((?:[^()]|\([^()]*\))*\)\s*\{`)
	for _, file := range sources {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if match := swallowed.Find(body); match != nil {
			t.Errorf("%s passes children to a Stamp, which drops them: %s", file, match)
		}
	}
}
