package gettext

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nmeilick/go-i18n/i18n"
	"github.com/nmeilick/go-i18n/locale"
)

// LoadFS loads PO files from fsys and compiles them into an i18n catalog.
func LoadFS(fsys fs.FS, pattern string, opts ...LoadOption) (*i18n.Catalog, error) {
	options := loadOptions{defaultLocale: "en"}
	for _, opt := range opts {
		opt(&options)
	}
	matches, err := fs.Glob(fsys, pattern)
	if err != nil {
		return nil, fmt.Errorf("glob %q: %w", pattern, err)
	}
	var entries []i18n.CatalogEntry
	for _, path := range matches {
		data, err := fsys.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", path, err)
		}
		doc, err := ParsePO(data)
		closeErr := data.Close()
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close %s: %w", path, closeErr)
		}
		loc := localeFromPath(path)
		compiled, err := compileDocument(loc, doc)
		if err != nil {
			return nil, fmt.Errorf("compile %s: %w", path, err)
		}
		entries = append(entries, compiled...)
	}
	return i18n.NewCatalog(entries, i18n.DefaultLocale(options.defaultLocale))
}

// LoadDir loads *.po files from dir.
func LoadDir(dir string, opts ...LoadOption) (*i18n.Catalog, error) {
	return LoadFS(os.DirFS(dir), "*.po", opts...)
}

type loadOptions struct {
	defaultLocale string
}

// LoadOption configures loading.
type LoadOption func(*loadOptions)

// DefaultLocale sets the default locale for compiled catalogs.
func DefaultLocale(tag string) LoadOption {
	return func(o *loadOptions) { o.defaultLocale = tag }
}

func localeFromPath(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = strings.ReplaceAll(base, "_", "-")
	return base
}

func compileDocument(loc string, doc *Document) ([]i18n.CatalogEntry, error) {
	header := doc.Header()
	if lang := header["Language"]; strings.TrimSpace(lang) != "" {
		loc = strings.TrimSpace(lang)
	}
	norm, err := locale.Normalize(loc)
	if err != nil {
		return nil, err
	}
	pluralHeader := header["Plural-Forms"]
	if pluralHeader == "" {
		if h, ok := PluralHeader(norm); ok {
			pluralHeader = h
		}
	}
	pluralRule, err := ParsePluralRule(pluralHeader)
	if err != nil {
		return nil, err
	}
	out := []i18n.CatalogEntry{}
	for _, entry := range doc.Entries {
		if entry.ID == "" || entry.Obsolete || hasFlag(entry, "fuzzy") {
			continue
		}
		out = append(out, i18n.CatalogEntry{
			Locale:       norm,
			Domain:       entry.Domain,
			Context:      entry.Context,
			ID:           entry.ID,
			PluralID:     entry.PluralID,
			Translations: append([]string(nil), entry.Strings...),
			PluralRule:   pluralRule,
		})
	}
	return out, nil
}
