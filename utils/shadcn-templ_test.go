package utils

import "testing"

func TestCNMergesClassesInInputOrder(t *testing.T) {
	for _, tc := range []struct {
		input []any
		want  string
	}{
		{[]any{"p-2 pb-11 px-4", "px-(--card-spacing)"}, "p-2 pb-11 px-(--card-spacing)"},
		{[]any{"p-2", "pb-11 px-4 px-(--card-spacing)"}, "p-2 pb-11 px-(--card-spacing)"},
		{[]any{"bg-red-500 hover:bg-blue-500 p-4!", []string{"bg-green-500", "p-2!"}}, "hover:bg-blue-500 bg-green-500 p-2!"},
		{[]any{"bg-red-500", []any{"hover:bg-blue-500 p-4! bg-green-500", map[string]bool{"p-2!": true, "p-8": false}}}, "hover:bg-blue-500 bg-green-500 p-2!"},
	} {
		if got := CN(tc.input...); got != tc.want {
			t.Errorf("CN(%v) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
