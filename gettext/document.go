package gettext

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Document is a semantic-lossless PO/POT document model. It preserves gettext
// metadata but normalizes formatting when written.
type Document struct {
	Entries []Entry
}

// Entry is one gettext entry.
type Entry struct {
	TranslatorComments []string
	ExtractedComments  []string
	References         []Reference
	Flags              []string
	Previous           Previous
	Obsolete           bool

	Context  string
	Domain   string
	ID       string
	PluralID string
	Strings  []string
}

// Reference is one source reference.
type Reference struct {
	File string
	Line int
}

// Previous stores #| previous ids.
type Previous struct {
	Context  string
	ID       string
	PluralID string
}

// Header returns the gettext header key/value map.
func (d *Document) Header() map[string]string {
	out := map[string]string{}
	for _, e := range d.Entries {
		if e.ID != "" {
			continue
		}
		for _, line := range strings.Split(e.stringAt(0), "\n") {
			if line == "" {
				continue
			}
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			out[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
		return out
	}
	return out
}

func (e Entry) stringAt(i int) string {
	if i < 0 || i >= len(e.Strings) {
		return ""
	}
	return e.Strings[i]
}

type parseState struct {
	doc       Document
	entry     Entry
	hasEntry  bool
	lastField string
	lastIndex int
	line      int
}

// ParsePO parses a PO document.
func ParsePO(r io.Reader) (*Document, error) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	st := parseState{lastIndex: -1}
	for s.Scan() {
		st.line++
		line := s.Text()
		if strings.TrimSpace(line) == "" {
			st.flush()
			continue
		}
		if err := st.parseLine(line); err != nil {
			return nil, err
		}
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("read PO: %w", err)
	}
	st.flush()
	return &st.doc, nil
}

// ParsePOT parses a POT document.
func ParsePOT(r io.Reader) (*Document, error) {
	return ParsePO(r)
}

func (s *parseState) flush() {
	if !s.hasEntry {
		s.entry = Entry{}
		s.lastField = ""
		s.lastIndex = -1
		return
	}
	s.doc.Entries = append(s.doc.Entries, s.entry)
	s.entry = Entry{}
	s.hasEntry = false
	s.lastField = ""
	s.lastIndex = -1
}

func (s *parseState) parseLine(line string) error {
	trim := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(trim, "#~"):
		s.hasEntry = true
		s.entry.Obsolete = true
		return s.parseKeywordLine(strings.TrimSpace(strings.TrimPrefix(trim, "#~")))
	case strings.HasPrefix(trim, "#|"):
		s.hasEntry = true
		return s.parsePrevious(strings.TrimSpace(strings.TrimPrefix(trim, "#|")))
	case strings.HasPrefix(trim, "#."):
		s.hasEntry = true
		comment := strings.TrimSpace(strings.TrimPrefix(trim, "#."))
		if domain, ok := strings.CutPrefix(comment, "Domain: "); ok {
			s.entry.Domain = strings.TrimSpace(domain)
		}
		s.entry.ExtractedComments = append(s.entry.ExtractedComments, comment)
		return nil
	case strings.HasPrefix(trim, "#:"):
		s.hasEntry = true
		s.entry.References = append(s.entry.References, parseReferences(strings.TrimSpace(strings.TrimPrefix(trim, "#:")))...)
		return nil
	case strings.HasPrefix(trim, "#,"):
		s.hasEntry = true
		for _, flag := range strings.Split(strings.TrimSpace(strings.TrimPrefix(trim, "#,")), ",") {
			flag = strings.TrimSpace(flag)
			if flag != "" {
				s.entry.Flags = append(s.entry.Flags, flag)
			}
		}
		return nil
	case strings.HasPrefix(trim, "#"):
		s.hasEntry = true
		s.entry.TranslatorComments = append(s.entry.TranslatorComments, strings.TrimSpace(strings.TrimPrefix(trim, "#")))
		return nil
	default:
		s.hasEntry = true
		return s.parseKeywordLine(trim)
	}
}

func parseReferences(s string) []Reference {
	fields := strings.Fields(s)
	out := make([]Reference, 0, len(fields))
	for _, field := range fields {
		file := field
		line := 0
		if idx := strings.LastIndex(field, ":"); idx >= 0 {
			file = field[:idx]
			line, _ = strconv.Atoi(field[idx+1:])
		}
		out = append(out, Reference{File: file, Line: line})
	}
	return out
}

func (s *parseState) parsePrevious(line string) error {
	key, value, err := parseKeyword(line)
	if err != nil {
		return fmt.Errorf("line %d: %w", s.line, err)
	}
	switch key {
	case "msgctxt":
		s.entry.Previous.Context = value
	case "msgid":
		s.entry.Previous.ID = value
	case "msgid_plural":
		s.entry.Previous.PluralID = value
	}
	return nil
}

func (s *parseState) parseKeywordLine(line string) error {
	if strings.HasPrefix(line, "\"") {
		value, err := parsePOString(line)
		if err != nil {
			return fmt.Errorf("line %d: %w", s.line, err)
		}
		switch s.lastField {
		case "msgctxt":
			s.entry.Context += value
		case "msgid":
			s.entry.ID += value
		case "msgid_plural":
			s.entry.PluralID += value
		case "msgstr":
			s.ensureString(s.lastIndex)
			s.entry.Strings[s.lastIndex] += value
		default:
			return fmt.Errorf("line %d: continued string without field", s.line)
		}
		return nil
	}
	key, value, err := parseKeyword(line)
	if err != nil {
		return fmt.Errorf("line %d: %w", s.line, err)
	}
	switch {
	case key == "msgctxt":
		s.entry.Context = value
		s.lastField = "msgctxt"
		s.lastIndex = -1
	case key == "msgid":
		s.entry.ID = value
		s.lastField = "msgid"
		s.lastIndex = -1
	case key == "msgid_plural":
		s.entry.PluralID = value
		s.lastField = "msgid_plural"
		s.lastIndex = -1
	case key == "msgstr":
		s.ensureString(0)
		s.entry.Strings[0] = value
		s.lastField = "msgstr"
		s.lastIndex = 0
	case strings.HasPrefix(key, "msgstr["):
		idxText := strings.TrimSuffix(strings.TrimPrefix(key, "msgstr["), "]")
		idx, err := strconv.Atoi(idxText)
		if err != nil || idx < 0 || idx >= maxNPlurals {
			return fmt.Errorf("invalid plural index %q", key)
		}
		s.ensureString(idx)
		s.entry.Strings[idx] = value
		s.lastField = "msgstr"
		s.lastIndex = idx
	default:
		return fmt.Errorf("unknown gettext keyword %q", key)
	}
	return nil
}

func (s *parseState) ensureString(idx int) {
	for len(s.entry.Strings) <= idx {
		s.entry.Strings = append(s.entry.Strings, "")
	}
}

func parseKeyword(line string) (string, string, error) {
	idx := strings.IndexFunc(line, func(r rune) bool { return r == ' ' || r == '\t' })
	if idx < 0 {
		return "", "", fmt.Errorf("missing gettext string")
	}
	key := strings.TrimSpace(line[:idx])
	value, err := parsePOString(strings.TrimSpace(line[idx+1:]))
	return key, value, err
}

func parsePOString(s string) (string, error) {
	value, err := strconv.Unquote(s)
	if err != nil {
		return "", fmt.Errorf("parse PO string %q: %w", s, err)
	}
	return value, nil
}

// WritePO writes doc deterministically.
func WritePO(w io.Writer, doc *Document, opts ...WriteOption) error {
	return writeDocument(w, doc, opts...)
}

// WritePOT writes doc deterministically.
func WritePOT(w io.Writer, doc *Document, opts ...WriteOption) error {
	return writeDocument(w, doc, opts...)
}

type writeOptions struct {
	sortEntries bool
}

// WriteOption configures document writing.
type WriteOption func(*writeOptions)

// PreserveOrder keeps entry order. By default entries are sorted except the header.
func PreserveOrder() WriteOption {
	return func(o *writeOptions) { o.sortEntries = false }
}

func writeDocument(w io.Writer, doc *Document, opts ...WriteOption) error {
	options := writeOptions{sortEntries: true}
	for _, opt := range opts {
		opt(&options)
	}
	if doc == nil {
		doc = &Document{}
	}
	entries := append([]Entry(nil), doc.Entries...)
	if options.sortEntries {
		sortEntries(entries)
	}
	for i, entry := range entries {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if err := writeEntry(w, entry); err != nil {
			return err
		}
	}
	return nil
}

func sortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].ID == "" {
			return true
		}
		if entries[j].ID == "" {
			return false
		}
		if entries[i].Context != entries[j].Context {
			return entries[i].Context < entries[j].Context
		}
		return entries[i].ID < entries[j].ID
	})
}

func writeEntry(w io.Writer, e Entry) error {
	prefix := ""
	if e.Obsolete {
		prefix = "#~ "
	}
	for _, c := range e.TranslatorComments {
		if _, err := fmt.Fprintf(w, "# %s\n", c); err != nil {
			return err
		}
	}
	for _, c := range e.ExtractedComments {
		if _, err := fmt.Fprintf(w, "#. %s\n", c); err != nil {
			return err
		}
	}
	if e.Domain != "" && !hasExtractedComment(e, "Domain: "+e.Domain) {
		if _, err := fmt.Fprintf(w, "#. Domain: %s\n", e.Domain); err != nil {
			return err
		}
	}
	if len(e.References) > 0 {
		parts := make([]string, 0, len(e.References))
		for _, r := range e.References {
			if r.Line > 0 {
				parts = append(parts, fmt.Sprintf("%s:%d", r.File, r.Line))
			} else {
				parts = append(parts, r.File)
			}
		}
		sort.Strings(parts)
		if _, err := fmt.Fprintf(w, "#: %s\n", strings.Join(parts, " ")); err != nil {
			return err
		}
	}
	if len(e.Flags) > 0 {
		flags := append([]string(nil), e.Flags...)
		sort.Strings(flags)
		if _, err := fmt.Fprintf(w, "#, %s\n", strings.Join(flags, ", ")); err != nil {
			return err
		}
	}
	if e.Previous.Context != "" {
		if _, err := fmt.Fprintf(w, "#| msgctxt %s\n", quote(e.Previous.Context)); err != nil {
			return err
		}
	}
	if e.Previous.ID != "" {
		if _, err := fmt.Fprintf(w, "#| msgid %s\n", quote(e.Previous.ID)); err != nil {
			return err
		}
	}
	if e.Previous.PluralID != "" {
		if _, err := fmt.Fprintf(w, "#| msgid_plural %s\n", quote(e.Previous.PluralID)); err != nil {
			return err
		}
	}
	if e.Context != "" {
		if _, err := fmt.Fprintf(w, "%smsgctxt %s\n", prefix, quote(e.Context)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "%smsgid %s\n", prefix, quote(e.ID)); err != nil {
		return err
	}
	if e.PluralID != "" {
		if _, err := fmt.Fprintf(w, "%smsgid_plural %s\n", prefix, quote(e.PluralID)); err != nil {
			return err
		}
		for i, s := range e.Strings {
			if _, err := fmt.Fprintf(w, "%smsgstr[%d] %s\n", prefix, i, quote(s)); err != nil {
				return err
			}
		}
	} else {
		if _, err := fmt.Fprintf(w, "%smsgstr %s\n", prefix, quote(e.stringAt(0))); err != nil {
			return err
		}
	}
	return nil
}

func hasExtractedComment(e Entry, comment string) bool {
	for _, c := range e.ExtractedComments {
		if c == comment {
			return true
		}
	}
	return false
}

func quote(s string) string {
	return strconv.Quote(s)
}

func hasFlag(e Entry, flag string) bool {
	for _, f := range e.Flags {
		if f == flag {
			return true
		}
	}
	return false
}
