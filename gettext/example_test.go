package gettext

import (
	"fmt"
	"strings"
)

func ExampleParsePO() {
	doc, err := ParsePO(strings.NewReader(`msgid ""
msgstr ""
"Language: de\n"
"Plural-Forms: nplurals=2; plural=(n != 1);\n"

msgid "Hello"
msgstr "Hallo"
`))
	if err != nil {
		panic(err)
	}

	fmt.Println(doc.Header()["Language"])
	fmt.Println(doc.Entries[1].Strings[0])

	// Output:
	// de
	// Hallo
}
