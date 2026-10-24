package utils

import "testing"

// CN memoizes by the space-joined classes, so differently split inputs with the
// same classes must merge alike, and a remembered result must match a fresh one.
func TestCNMemoMatchesMerge(t *testing.T) {
	inputs := [][]any{
		{"p-2 pb-11", "px-(--card-spacing)"},
		{"p-2", "pb-11 px-(--card-spacing)"},
		{"bg-red-500 hover:bg-blue-500", []string{"bg-green-500", "p-2!"}},
		{"bg-red-500", "hover:bg-blue-500 bg-green-500", map[string]bool{"p-2!": true, "p-4": false}},
	}
	for _, in := range inputs {
		want := cn(flattenClasses(in))
		for range 2 {
			if got := CN(in...); got != want {
				t.Errorf("CN(%q) = %q, want %q", in, got, want)
			}
		}
	}
	if a, b := CN(inputs[0]...), CN(inputs[1]...); a != b {
		t.Errorf("same classes merged differently: %q vs %q", a, b)
	}
	if a, b := CN(inputs[2]...), CN(inputs[3]...); a != b {
		t.Errorf("same classes merged differently: %q vs %q", a, b)
	}
}
