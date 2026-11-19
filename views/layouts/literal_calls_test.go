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
