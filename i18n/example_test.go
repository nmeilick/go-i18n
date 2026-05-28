package i18n

import "fmt"

func ExampleLocalizer_T() {
	cat, err := NewCatalog([]CatalogEntry{{
		Locale:       "de",
		ID:           "Hello {name}",
		Translations: []string{"Hallo {name}"},
	}}, DefaultLocale("en"))
	if err != nil {
		panic(err)
	}

	tr := NewRuntime(cat).Localizer("de")
	fmt.Println(tr.T("Hello {name}", Arg("name", "Ada")))

	// Output:
	// Hallo Ada
}
