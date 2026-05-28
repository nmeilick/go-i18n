package extract

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/nmeilick/go-i18n/gettext"
)

type ValueSpec struct {
	Arg     int
	Literal string
}

type Keyword struct {
	Target  string
	Msg     ValueSpec
	Plural  ValueSpec
	Context ValueSpec
	Count   ValueSpec
	Vars    ValueSpec
	Domain  ValueSpec
}

func newKeyword(target string) Keyword {
	return Keyword{
		Target:  target,
		Msg:     ValueSpec{Arg: -1},
		Plural:  ValueSpec{Arg: -1},
		Context: ValueSpec{Arg: -1},
		Count:   ValueSpec{Arg: -1},
		Vars:    ValueSpec{Arg: -1},
		Domain:  ValueSpec{Arg: -1},
	}
}

type Warning struct {
	Code string `json:"code"`
	Path string `json:"path,omitempty"`
	Line int    `json:"line,omitempty"`
	Msg  string `json:"message"`
}

type Report struct {
	Messages int
	Warnings []Warning
}

type Options struct {
	Roots    []string
	Keywords []Keyword
	Strict   bool
}

func DefaultKeywords() []Keyword {
	t := newKeyword("T")
	t.Msg = ValueSpec{Arg: 0}
	t.Vars = ValueSpec{Arg: 1}
	tn := newKeyword("Tn")
	tn.Count = ValueSpec{Arg: 0}
	tn.Msg = ValueSpec{Arg: 1}
	tn.Plural = ValueSpec{Arg: 2}
	tn.Vars = ValueSpec{Arg: 3}
	tc := newKeyword("Tc")
	tc.Context = ValueSpec{Arg: 0}
	tc.Msg = ValueSpec{Arg: 1}
	tc.Vars = ValueSpec{Arg: 2}
	tnc := newKeyword("Tnc")
	tnc.Context = ValueSpec{Arg: 0}
	tnc.Count = ValueSpec{Arg: 1}
	tnc.Msg = ValueSpec{Arg: 2}
	tnc.Plural = ValueSpec{Arg: 3}
	tnc.Vars = ValueSpec{Arg: 4}
	return []Keyword{
		t,
		tn,
		tc,
		tnc,
	}
}

func ParseKeyword(spec string) (Keyword, error) {
	target, rest, ok := strings.Cut(spec, ":")
	if !ok {
		kw := newKeyword(strings.TrimSpace(spec))
		kw.Msg = ValueSpec{Arg: 0}
		return kw, nil
	}
	kw := newKeyword(strings.TrimSpace(target))
	for _, part := range strings.Split(rest, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			return Keyword{}, fmt.Errorf("invalid keyword role %q", part)
		}
		vs, err := parseValueSpec(value)
		if err != nil {
			return Keyword{}, err
		}
		switch strings.TrimSpace(key) {
		case "msg":
			kw.Msg = vs
		case "plural":
			kw.Plural = vs
		case "ctx":
			kw.Context = vs
		case "count":
			kw.Count = vs
		case "vars":
			kw.Vars = vs
		case "domain":
			kw.Domain = vs
		default:
			return Keyword{}, fmt.Errorf("unknown keyword role %q", key)
		}
	}
	return kw, nil
}

func parseValueSpec(value string) (ValueSpec, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
		return ValueSpec{Literal: strings.Trim(value, "'")}, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return ValueSpec{}, fmt.Errorf("invalid argument index %q", value)
	}
	return ValueSpec{Arg: n - 1}, nil
}

func Extract(ctx context.Context, opts Options) (*gettext.Document, Report, error) {
	if len(opts.Roots) == 0 {
		opts.Roots = []string{"."}
	}
	if len(opts.Keywords) == 0 {
		opts.Keywords = DefaultKeywords()
	}
	var entries []gettext.Entry
	report := Report{}
	for _, root := range opts.Roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() {
				base := d.Name()
				if path != root && (base == ".git" || base == "vendor" || base == "testdata" || strings.HasPrefix(base, ".")) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fileEntries, warnings, err := extractFile(path, opts.Keywords)
			report.Warnings = append(report.Warnings, warnings...)
			if err != nil {
				return err
			}
			entries = append(entries, fileEntries...)
			return nil
		})
		if err != nil {
			return nil, report, err
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Domain != entries[j].Domain {
			return entries[i].Domain < entries[j].Domain
		}
		if entries[i].Context != entries[j].Context {
			return entries[i].Context < entries[j].Context
		}
		if entries[i].ID != entries[j].ID {
			return entries[i].ID < entries[j].ID
		}
		return entries[i].PluralID < entries[j].PluralID
	})
	doc := &gettext.Document{}
	seen := map[string]int{}
	for _, entry := range entries {
		key := entryKey(entry)
		if idx, ok := seen[key]; ok {
			doc.Entries[idx] = mergeExtractedEntry(doc.Entries[idx], entry)
			continue
		}
		seen[key] = len(doc.Entries)
		doc.Entries = append(doc.Entries, entry)
	}
	report.Messages = len(doc.Entries)
	if opts.Strict && len(report.Warnings) > 0 {
		return doc, report, fmt.Errorf("strict extraction failed with %d warnings", len(report.Warnings))
	}
	return doc, report, nil
}

func extractFile(path string, keywords []Keyword) ([]gettext.Entry, []Warning, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}
	commentByLine := translatorComments(fset, file)
	var entries []gettext.Entry
	var warnings []Warning
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := callPath(call.Fun)
		if name == "" {
			return true
		}
		for _, kw := range keywords {
			if !keywordMatches(kw.Target, name) {
				continue
			}
			pos := fset.Position(call.Pos())
			entry, ws := extractCall(call, kw, path, pos.Line, commentByLine[pos.Line-1])
			warnings = append(warnings, ws...)
			if entry.ID != "" {
				entries = append(entries, entry)
			}
			return true
		}
		return true
	})
	return entries, warnings, nil
}

func translatorComments(fset *token.FileSet, file *ast.File) map[int][]string {
	out := map[int][]string{}
	for _, group := range file.Comments {
		end := fset.Position(group.End()).Line
		for _, c := range group.List {
			text := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(c.Text, "//"), "/*"))
			text = strings.TrimSuffix(text, "*/")
			text = strings.TrimSpace(text)
			if strings.HasPrefix(text, "TRANSLATORS:") {
				out[end] = append(out[end], strings.TrimSpace(strings.TrimPrefix(text, "TRANSLATORS:")))
			}
		}
	}
	return out
}

func callName(expr ast.Expr) string {
	name := callPath(expr)
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return name
}

func callPath(expr ast.Expr) string {
	switch x := expr.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		prefix := callPath(x.X)
		if prefix == "" {
			return x.Sel.Name
		}
		return prefix + "." + x.Sel.Name
	default:
		return ""
	}
}

func keywordMatches(target, name string) bool {
	target = strings.TrimSpace(target)
	name = strings.TrimSpace(name)
	if target == "" || name == "" {
		return false
	}
	if strings.Contains(target, ".") {
		return target == name
	}
	if target == name {
		return true
	}
	i := strings.LastIndexByte(name, '.')
	return i >= 0 && name[i+1:] == target
}

func entryKey(entry gettext.Entry) string {
	return strings.Join([]string{entry.Domain, entry.Context, entry.ID, entry.PluralID}, "\x00")
}

func mergeExtractedEntry(dst, src gettext.Entry) gettext.Entry {
	dst.References = appendUniqueRefs(dst.References, src.References)
	dst.ExtractedComments = appendUniqueStrings(dst.ExtractedComments, src.ExtractedComments)
	dst.Flags = appendUniqueStrings(dst.Flags, src.Flags)
	return dst
}

func appendUniqueStrings(dst, src []string) []string {
	seen := make(map[string]struct{}, len(dst)+len(src))
	for _, value := range dst {
		seen[value] = struct{}{}
	}
	for _, value := range src {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		dst = append(dst, value)
	}
	return dst
}

func appendUniqueRefs(dst, src []gettext.Reference) []gettext.Reference {
	type key struct {
		file string
		line int
	}
	seen := make(map[key]struct{}, len(dst)+len(src))
	for _, ref := range dst {
		seen[key{file: ref.File, line: ref.Line}] = struct{}{}
	}
	for _, ref := range src {
		k := key{file: ref.File, line: ref.Line}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		dst = append(dst, ref)
	}
	return dst
}

func extractCall(call *ast.CallExpr, kw Keyword, path string, line int, comments []string) (gettext.Entry, []Warning) {
	var warnings []Warning
	msg, ok := literalArg(call, kw.Msg)
	if !ok {
		return gettext.Entry{}, []Warning{{Code: "dynamic_message", Path: path, Line: line, Msg: "message argument is not a string literal"}}
	}
	plural, _ := literalArg(call, kw.Plural)
	ctx, _ := literalArg(call, kw.Context)
	domain, _ := literalArg(call, kw.Domain)
	entry := gettext.Entry{
		Context:           ctx,
		Domain:            domain,
		ID:                msg,
		PluralID:          plural,
		ExtractedComments: append([]string(nil), comments...),
		References:        []gettext.Reference{{File: path, Line: line}},
		Flags:             []string{"go-i18n-brace-format"},
	}
	if plural != "" {
		entry.Strings = []string{"", ""}
	} else {
		entry.Strings = []string{""}
	}
	entry.ExtractedComments = append(entry.ExtractedComments, placeholderComments(call, msg, plural, kw)...)
	return entry, warnings
}

func literalArg(call *ast.CallExpr, spec ValueSpec) (string, bool) {
	if spec.Literal != "" {
		return spec.Literal, true
	}
	if spec.Arg < 0 || spec.Arg >= len(call.Args) {
		return "", false
	}
	lit, ok := call.Args[spec.Arg].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}

func placeholderComments(call *ast.CallExpr, msg, plural string, kw Keyword) []string {
	varsArg := kw.Vars.Arg
	if varsArg < 0 || varsArg >= len(call.Args) {
		return nil
	}
	meta := map[string]string{}
	if lit, ok := call.Args[varsArg].(*ast.CompositeLit); ok {
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			keyLit, ok := kv.Key.(*ast.BasicLit)
			if !ok || keyLit.Kind != token.STRING {
				continue
			}
			key, err := strconv.Unquote(keyLit.Value)
			if err != nil {
				continue
			}
			meta[key] = valueType(kv.Value)
		}
	}
	placeholders := placeholders(msg + " " + plural)
	out := make([]string, 0, len(placeholders))
	for _, name := range placeholders {
		typ := meta[name]
		if typ == "" {
			typ = "value"
		}
		out = append(out, fmt.Sprintf("Placeholder {%s}: %s.", name, typ))
	}
	return out
}

func valueType(expr ast.Expr) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return "value"
	}
	switch callName(call.Fun) {
	case "Currency":
		return "currency"
	case "Number":
		return "number"
	case "Percent":
		return "percent"
	case "Date":
		return "date"
	case "Time":
		return "time"
	case "DateTime":
		return "datetime"
	case "Duration":
		return "duration"
	case "List":
		return "list"
	case "Unit":
		return "unit"
	default:
		return "value"
	}
}

func placeholders(s string) []string {
	seen := map[string]bool{}
	var out []string
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		end := strings.IndexByte(s[i+1:], '}')
		if end < 0 {
			continue
		}
		name := s[i+1 : i+1+end]
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
		i += end + 1
	}
	sort.Strings(out)
	return out
}
