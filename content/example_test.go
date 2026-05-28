package content

import (
	"encoding/json"
	"fmt"

	"github.com/nmeilick/go-i18n/i18n"
)

func ExampleProjectField() {
	res := i18n.Result{Text: "Email is required", MessageLocale: "en"}
	payload := FieldMessage{
		Field:   "email",
		Path:    "user.email",
		Code:    "required",
		Message: MessageFromResult("Email is required", res),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}

	fmt.Println(string(data))

	// Output:
	// {"field":"email","path":"user.email","code":"required","message":{"text":"Email is required","locale":"en"}}
}
