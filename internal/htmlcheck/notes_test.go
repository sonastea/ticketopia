package htmlcheck

import (
	"reflect"
	"strings"
	"testing"
)

func TestNotesOverBudgetCountsOnlySectionNotes(t *testing.T) {
	page := `<main>
		<section id="crowded"><p class="field-note">One.</p><p class="field-note">Two.</p></section>
		<section id="borrowed"><p class="field-note">One.</p><p id="far-help" class="field-note">Not this section's field.</p></section>
		<section id="labelled"><div aria-describedby="div-help"></div><p class="field-note">One.</p><p id="div-help" class="field-note">Not a control.</p></section>
		<form><input aria-describedby="far-help div-help"></form>
		<form id="fields">
			<p class="field-note">Section note.</p>
			<input aria-describedby="zone-help"><p id="zone-help" class="field-note">Field help.</p>
			<ul><li><p class="field-note">Item metadata.</p></li><li><p class="field-note">More.</p></li></ul>
			<p class="field-note" role="status"></p>
			<details><p class="field-note">Inside its own section.</p></details>
		</form>
	</main>`
	over, err := NotesOverBudget(strings.NewReader(page), 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`<section id="crowded"> 2 notes`, `<section id="borrowed"> 2 notes`, `<section id="labelled"> 2 notes`}
	if !reflect.DeepEqual(over, want) {
		t.Fatalf("got %v, want %v", over, want)
	}
}
