package accounts

import "testing"

func TestExplicitActivityName(t *testing.T) {
	for raw, want := range map[string]string{"": "", "  Alex  ": "Alex", " Jean   Luc ": "Jean Luc", "علي": "علي", "李": "李", "Anne-Marie": "Anne-Marie", "D’Arcy": "D’Arcy", "Jose\u0301": "Jose\u0301"} {
		got, err := ActivityFirstName(raw)
		if err != nil || got != want {
			t.Errorf("name normalization failed: %q %v", got, err)
		}
	}
	for _, raw := range []string{"alex@example.com", "https://alex.test", "Alex1", "<script>", "Alex\u202e", "Alex\n", "---", "\xff"} {
		if _, err := ActivityFirstName(raw); err == nil {
			t.Errorf("accepted invalid attribution %q", raw)
		}
	}
}
