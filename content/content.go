package content

import "github.com/nmeilick/go-i18n/i18n"

// Message is an embeddable localized message payload.
type Message struct {
	ID     string      `json:"id,omitempty"`
	Text   string      `json:"text"`
	Locale string      `json:"locale,omitempty"`
	Lookup *LookupMeta `json:"lookup,omitempty"`
}

// LookupMeta contains explicit lookup state.
type LookupMeta struct {
	Status          string   `json:"status,omitempty"`
	RequestedLocale string   `json:"requested_locale,omitempty"`
	ResolvedLocale  string   `json:"resolved_locale,omitempty"`
	MessageLocale   string   `json:"message_locale,omitempty"`
	Domain          string   `json:"domain,omitempty"`
	FallbackChain   []string `json:"fallback_chain,omitempty"`
}

// FieldMessage is a reusable field-level localized payload.
type FieldMessage struct {
	Field   string  `json:"field,omitempty"`
	Path    string  `json:"path,omitempty"`
	Code    string  `json:"code,omitempty"`
	Message Message `json:"message"`
}

// MessageFromResult projects an i18n result into a public content message.
func MessageFromResult(id string, res i18n.Result) Message {
	return Message{
		Text:   res.Text,
		Locale: res.MessageLocale,
	}
}

// MessageFromResultWithLookup projects an i18n result with diagnostic lookup metadata.
func MessageFromResultWithLookup(id string, res i18n.Result) Message {
	msg := MessageFromResult(id, res)
	msg.ID = id
	msg.Lookup = lookupMeta(res)
	return msg
}

// Project looks up msg and projects it into a public message payload.
func Project(tr *i18n.Localizer, msg i18n.Message, vars ...i18n.Vars) Message {
	res := tr.Lookup(msg, vars...)
	return MessageFromResult(msg.ID, res)
}

// ProjectWithLookup looks up msg and projects it with diagnostic lookup metadata.
func ProjectWithLookup(tr *i18n.Localizer, msg i18n.Message, vars ...i18n.Vars) Message {
	res := tr.Lookup(msg, vars...)
	return MessageFromResultWithLookup(msg.ID, res)
}

// ProjectField looks up msg and projects a public field-level message.
func ProjectField(tr *i18n.Localizer, field, path, code string, msg i18n.Message, vars ...i18n.Vars) FieldMessage {
	return FieldMessage{
		Field:   field,
		Path:    path,
		Code:    code,
		Message: Project(tr, msg, vars...),
	}
}

// ProjectFieldWithLookup looks up msg and projects a field-level message with diagnostic lookup metadata.
func ProjectFieldWithLookup(tr *i18n.Localizer, field, path, code string, msg i18n.Message, vars ...i18n.Vars) FieldMessage {
	return FieldMessage{
		Field:   field,
		Path:    path,
		Code:    code,
		Message: ProjectWithLookup(tr, msg, vars...),
	}
}

func lookupMeta(res i18n.Result) *LookupMeta {
	return &LookupMeta{
		Status:          string(res.Status),
		RequestedLocale: res.RequestedLocale,
		ResolvedLocale:  res.ResolvedLocale,
		MessageLocale:   res.MessageLocale,
		Domain:          res.Domain,
		FallbackChain:   append([]string(nil), res.FallbackChain...),
	}
}
