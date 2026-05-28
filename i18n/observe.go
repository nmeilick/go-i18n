package i18n

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/nmeilick/go-i18n/observe"
)

// MessageIdentityPolicy controls message-key attributes in lookup events.
type MessageIdentityPolicy string

const (
	// MessageIdentityRaw includes bounded raw msgid, msgctxt, and plural id.
	MessageIdentityRaw MessageIdentityPolicy = "raw"
	// MessageIdentityHash includes only a deterministic message fingerprint.
	MessageIdentityHash MessageIdentityPolicy = "hash"
	// MessageIdentityOmit omits message identity attributes.
	MessageIdentityOmit MessageIdentityPolicy = "omit"
)

// ObservePolicy controls i18n event emission.
type ObservePolicy struct {
	IncludeExactLookups bool
	SuppressFallbacks   bool
	SuppressMissing     bool
	SuppressDiagnostics bool
	MessageIdentity     MessageIdentityPolicy
	EventPolicy         observe.Policy
}

// DefaultObservePolicy returns the default event policy.
func DefaultObservePolicy() ObservePolicy {
	return ObservePolicy{
		MessageIdentity: MessageIdentityRaw,
		EventPolicy:     observe.DefaultPolicy(),
	}
}

func (p ObservePolicy) normalized() ObservePolicy {
	if p.MessageIdentity == "" {
		p.MessageIdentity = MessageIdentityRaw
	}
	if p.EventPolicy == (observe.Policy{}) {
		p.EventPolicy = observe.DefaultPolicy()
	}
	return p
}

func (p ObservePolicy) shouldEmitLookup(result Result) bool {
	switch result.Status {
	case StatusMissing:
		return !p.SuppressMissing
	case StatusFallback:
		return !p.SuppressFallbacks
	case StatusExact:
		if len(result.Diagnostics) > 0 {
			return !p.SuppressDiagnostics
		}
		return p.IncludeExactLookups
	default:
		return true
	}
}

func (m Message) fingerprint() string {
	h := sha256.New()
	parts := []string{m.Domain, m.Context, m.ID, m.PluralID}
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	if m.HasPlural {
		h.Write([]byte("plural"))
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:16])
}
