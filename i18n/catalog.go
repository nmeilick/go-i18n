package i18n

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/nmeilick/go-i18n/locale"
	"golang.org/x/text/language"
)

// PluralSelector selects a gettext plural index for n.
type PluralSelector func(n int64) int

// PluralRule describes a locale plural selector.
type PluralRule struct {
	NPlurals int
	Header   string
	Select   PluralSelector
}

// EnglishPluralRule returns the common two-form English selector.
func EnglishPluralRule() PluralRule {
	return PluralRule{
		NPlurals: 2,
		Header:   "nplurals=2; plural=(n != 1);",
		Select: func(n int64) int {
			if n != 1 {
				return 1
			}
			return 0
		},
	}
}

// CatalogEntry is an input entry for building an immutable Catalog.
type CatalogEntry struct {
	Locale       string
	Domain       string
	Context      string
	ID           string
	PluralID     string
	Translations []string
	PluralRule   PluralRule
}

type compiledEntry struct {
	context      string
	id           string
	pluralID     string
	translations []string
}

type localeCatalog struct {
	tag     language.Tag
	plural  PluralRule
	domains map[string]map[messageKey]compiledEntry
}

type catalogSnapshot struct {
	defaultLocale string
	version       string
	locales       map[string]localeCatalog
	supported     []string
	negotiator    *locale.Negotiator
}

// Catalog is a deeply immutable compiled message catalog.
type Catalog struct {
	snapshot *catalogSnapshot
}

// CatalogOption configures catalog construction.
type CatalogOption func(*catalogOptions) error

type catalogOptions struct {
	defaultLocale string
	version       string
}

// DefaultLocale sets the catalog default locale.
func DefaultLocale(tag string) CatalogOption {
	return func(o *catalogOptions) error {
		o.defaultLocale = tag
		return nil
	}
}

// Version sets a catalog version. If omitted, a stable content checksum is used.
func Version(version string) CatalogOption {
	return func(o *catalogOptions) error {
		o.version = strings.TrimSpace(version)
		return nil
	}
}

// NewCatalog builds an immutable catalog from entries.
func NewCatalog(entries []CatalogEntry, opts ...CatalogOption) (*Catalog, error) {
	options := catalogOptions{defaultLocale: "en"}
	for _, opt := range opts {
		if err := opt(&options); err != nil {
			return nil, err
		}
	}
	if options.defaultLocale == "" {
		options.defaultLocale = "en"
	}
	defaultTag, err := locale.Parse(options.defaultLocale)
	if err != nil {
		return nil, err
	}
	options.defaultLocale = defaultTag.String()
	locales := map[string]localeCatalog{}
	for i, entry := range entries {
		if strings.TrimSpace(entry.Locale) == "" {
			return nil, fmt.Errorf("catalog entry %d: locale is required", i)
		}
		tag, err := locale.Parse(entry.Locale)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(entry.ID) == "" {
			return nil, fmt.Errorf("catalog entry %d: id is required", i)
		}
		translations := append([]string(nil), entry.Translations...)
		if len(translations) == 0 {
			continue
		}
		plural := entry.PluralRule
		if plural.Select == nil || plural.NPlurals <= 0 {
			plural = EnglishPluralRule()
		}
		if entry.PluralID != "" && len(translations) != plural.NPlurals {
			return nil, fmt.Errorf("catalog entry %d: plural message %s %q has %d translations, want %d", i, tag.String(), entry.ID, len(translations), plural.NPlurals)
		}
		key := tag.String()
		lc, ok := locales[key]
		if !ok {
			lc = localeCatalog{
				tag:     tag,
				plural:  plural,
				domains: map[string]map[messageKey]compiledEntry{},
			}
		} else if !samePluralRule(lc.plural, plural) {
			return nil, fmt.Errorf("catalog entry %d: conflicting plural rule for locale %s", i, key)
		}
		domain := cleanDomain(entry.Domain)
		if lc.domains[domain] == nil {
			lc.domains[domain] = map[messageKey]compiledEntry{}
		}
		msgKey := keyFor(entry.Context, entry.ID)
		if existing, ok := lc.domains[domain][msgKey]; ok && existing.pluralID != entry.PluralID {
			return nil, fmt.Errorf("catalog entry %d: conflicting plural id for %s %q", i, key, entry.ID)
		}
		if _, ok := lc.domains[domain][msgKey]; ok {
			return nil, fmt.Errorf("catalog entry %d: duplicate message %s %q", i, key, entry.ID)
		}
		lc.domains[domain][msgKey] = compiledEntry{
			context:      entry.Context,
			id:           entry.ID,
			pluralID:     entry.PluralID,
			translations: translations,
		}
		locales[key] = lc
	}
	if len(locales) == 0 {
		tag := defaultTag
		locales[tag.String()] = localeCatalog{
			tag:     tag,
			plural:  EnglishPluralRule(),
			domains: map[string]map[messageKey]compiledEntry{},
		}
	}
	supported := make([]string, 0, len(locales))
	for tag := range locales {
		supported = append(supported, tag)
	}
	sort.Strings(supported)
	neg, err := locale.NewNegotiator(options.defaultLocale, supported)
	if err != nil {
		return nil, err
	}
	version := options.version
	if version == "" {
		version = checksum(entries)
	}
	snap := &catalogSnapshot{
		defaultLocale: options.defaultLocale,
		version:       version,
		locales:       locales,
		supported:     supported,
		negotiator:    neg,
	}
	return &Catalog{snapshot: snap}, nil
}

func checksum(entries []CatalogEntry) string {
	h := sha256.New()
	records := make([]string, 0, len(entries))
	for _, e := range entries {
		localeKey := strings.TrimSpace(e.Locale)
		if tag, err := locale.Parse(localeKey); err == nil {
			localeKey = tag.String()
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s\x00%s\x00%s\x00%s\x00%s\x00", localeKey, cleanDomain(e.Domain), e.Context, e.ID, e.PluralID)
		if e.PluralID != "" {
			fmt.Fprintf(&b, "nplurals=%d\x00plural=%s\x00", e.PluralRule.NPlurals, strings.TrimSpace(e.PluralRule.Header))
		}
		for _, t := range e.Translations {
			fmt.Fprintf(&b, "%s\x00", t)
		}
		records = append(records, b.String())
	}
	sort.Strings(records)
	for _, record := range records {
		h.Write([]byte(record))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func samePluralRule(a, b PluralRule) bool {
	if a.NPlurals != b.NPlurals {
		return false
	}
	aHeader := strings.TrimSpace(a.Header)
	bHeader := strings.TrimSpace(b.Header)
	return aHeader == "" || bHeader == "" || aHeader == bHeader
}

// Version returns the catalog version.
func (c *Catalog) Version() string {
	if c == nil || c.snapshot == nil {
		return ""
	}
	return c.snapshot.version
}

// Locales returns supported locales.
func (c *Catalog) Locales() []string {
	if c == nil || c.snapshot == nil {
		return nil
	}
	out := make([]string, len(c.snapshot.supported))
	copy(out, c.snapshot.supported)
	return out
}

func (c *Catalog) snap() *catalogSnapshot {
	if c == nil || c.snapshot == nil {
		cat, _ := NewCatalog(nil)
		return cat.snapshot
	}
	return c.snapshot
}
