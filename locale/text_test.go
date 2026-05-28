package locale

import (
	"reflect"
	"testing"

	"golang.org/x/text/language"
)

func TestTextHelpers(t *testing.T) {
	sv := ProfileForTag(language.Swedish)
	got := SortStrings(sv, []string{"z", "å", "ä", "a"})
	want := []string{"a", "z", "å", "ä"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("swedish sort = %#v", got)
	}
	tr := ProfileForTag(language.Turkish)
	if got := Lower(tr, "Iİ"); got != "ıi" {
		t.Fatalf("turkish lower = %q", got)
	}
	if !Contains(ProfileForTag(language.German), "Die Straße ist lang", "Straße") {
		t.Fatalf("german search did not match")
	}
	versions := TextVersions()
	if versions.CollateCLDR == "" || versions.CasesUnicode == "" {
		t.Fatalf("versions = %#v", versions)
	}
}

func TestValidateTextExtension(t *testing.T) {
	if !ValidateTextExtension("co", "phonebk") {
		t.Fatal("expected known collation extension")
	}
	if ValidateTextExtension("co", "not-real") {
		t.Fatal("unexpected collation extension")
	}
}
