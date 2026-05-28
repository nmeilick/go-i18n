package locale

import "testing"

func FuzzNormalize(f *testing.F) {
	f.Add("en")
	f.Add("pt_BR")
	f.Add("de-CH")
	f.Fuzz(func(t *testing.T, input string) {
		_, _ = Normalize(input)
	})
}
