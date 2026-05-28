package gettext

import (
	"strings"
	"testing"
)

func FuzzParsePO(f *testing.F) {
	f.Add(`msgid "Hello"` + "\n" + `msgstr "Hallo"` + "\n")
	f.Add(samplePO)
	f.Fuzz(func(t *testing.T, input string) {
		_, _ = ParsePO(strings.NewReader(input))
	})
}

func FuzzPluralRule(f *testing.F) {
	f.Add("nplurals=2; plural=(n != 1);")
	f.Add("nplurals=1; plural=0;")
	f.Fuzz(func(t *testing.T, input string) {
		rule, err := ParsePluralRule(input)
		if err == nil {
			_ = rule.Select(3)
		}
	})
}
