package gettext

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/nmeilick/go-i18n/i18n"
)

const (
	maxPluralHeaderBytes = 4096
	maxPluralExprBytes   = 1024
	maxPluralTokens      = 256
	maxPluralDepth       = 64
	maxNPlurals          = 32
)

// PluralHeader returns a conservative default plural header for locale.
func PluralHeader(locale string) (string, bool) {
	base := strings.ToLower(strings.Split(strings.ReplaceAll(locale, "_", "-"), "-")[0])
	switch base {
	case "ar":
		return "nplurals=6; plural=(n==0 ? 0 : n==1 ? 1 : n==2 ? 2 : n%100>=3 && n%100<=10 ? 3 : n%100>=11 && n%100<=99 ? 4 : 5);", true
	case "be", "ru", "uk":
		return "nplurals=3; plural=(n%10==1 && n%100!=11 ? 0 : n%10>=2 && n%10<=4 && (n%100<12 || n%100>14) ? 1 : 2);", true
	case "cs", "sk":
		return "nplurals=3; plural=(n==1 ? 0 : n>=2 && n<=4 ? 1 : 2);", true
	case "pl":
		return "nplurals=3; plural=(n==1 ? 0 : n%10>=2 && n%10<=4 && (n%100<12 || n%100>14) ? 1 : 2);", true
	case "fr":
		return "nplurals=2; plural=(n > 1);", true
	case "ja", "ko", "zh":
		return "nplurals=1; plural=0;", true
	default:
		return "nplurals=2; plural=(n != 1);", true
	}
}

// ParsePluralRule parses a gettext Plural-Forms header.
func ParsePluralRule(header string) (i18n.PluralRule, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return i18n.EnglishPluralRule(), nil
	}
	if len(header) > maxPluralHeaderBytes {
		return i18n.PluralRule{}, fmt.Errorf("plural header too large")
	}
	parts := strings.Split(header, ";")
	nplurals := 0
	expr := ""
	for _, part := range parts {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "nplurals":
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n <= 0 || n > maxNPlurals {
				return i18n.PluralRule{}, fmt.Errorf("invalid nplurals")
			}
			nplurals = n
		case "plural":
			expr = strings.TrimSpace(value)
		}
	}
	if nplurals <= 0 {
		return i18n.PluralRule{}, fmt.Errorf("missing nplurals")
	}
	if expr == "" {
		return i18n.PluralRule{}, fmt.Errorf("missing plural expression")
	}
	if len(expr) > maxPluralExprBytes {
		return i18n.PluralRule{}, fmt.Errorf("plural expression too large")
	}
	p := pluralParser{s: expr}
	node, err := p.parse()
	if err != nil {
		return i18n.PluralRule{}, err
	}
	return i18n.PluralRule{
		NPlurals: nplurals,
		Header:   header,
		Select: func(n int64) int {
			value := node.eval(n)
			if value < 0 {
				return 0
			}
			if value >= int64(nplurals) {
				return nplurals - 1
			}
			return int(value)
		},
	}, nil
}

type pluralParser struct {
	s      string
	pos    int
	tokens int
	depth  int
	err    error
}

type pluralNode interface {
	eval(n int64) int64
}

type pluralConst int64

func (c pluralConst) eval(int64) int64 { return int64(c) }

type pluralVar struct{}

func (pluralVar) eval(n int64) int64 { return n }

type pluralUnary struct {
	op string
	x  pluralNode
}

func (u pluralUnary) eval(n int64) int64 {
	v := u.x.eval(n)
	switch u.op {
	case "!":
		return boolInt(v == 0)
	case "-":
		if v == math.MinInt64 {
			return math.MaxInt64
		}
		return -v
	default:
		return 0
	}
}

type pluralBinary struct {
	op          string
	left, right pluralNode
}

func (b pluralBinary) eval(n int64) int64 {
	left := b.left.eval(n)
	right := b.right.eval(n)
	switch b.op {
	case "||":
		return boolInt(left != 0 || right != 0)
	case "&&":
		return boolInt(left != 0 && right != 0)
	case "==":
		return boolInt(left == right)
	case "!=":
		return boolInt(left != right)
	case "<=":
		return boolInt(left <= right)
	case ">=":
		return boolInt(left >= right)
	case "<":
		return boolInt(left < right)
	case ">":
		return boolInt(left > right)
	case "+":
		return left + right
	case "-":
		return left - right
	case "*":
		return left * right
	case "/":
		if right == 0 {
			return 0
		}
		return left / right
	case "%":
		if right == 0 {
			return 0
		}
		return left % right
	default:
		return 0
	}
}

type pluralTernary struct {
	cond, ifTrue, ifFalse pluralNode
}

func (t pluralTernary) eval(n int64) int64 {
	if t.cond.eval(n) != 0 {
		return t.ifTrue.eval(n)
	}
	return t.ifFalse.eval(n)
}

func (p *pluralParser) parse() (pluralNode, error) {
	v, err := p.ternary()
	if err != nil {
		return nil, err
	}
	if p.err != nil {
		return nil, p.err
	}
	p.skip()
	if p.pos != len(p.s) {
		return nil, fmt.Errorf("unexpected plural expression input")
	}
	return v, nil
}

func (p *pluralParser) ternary() (pluralNode, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	cond, err := p.or()
	if err != nil {
		return nil, err
	}
	p.skip()
	if !p.consume("?") {
		return cond, nil
	}
	a, err := p.ternary()
	if err != nil {
		return nil, err
	}
	if !p.consume(":") {
		return nil, fmt.Errorf("missing ':'")
	}
	b, err := p.ternary()
	if err != nil {
		return nil, err
	}
	return pluralTernary{cond: cond, ifTrue: a, ifFalse: b}, nil
}

func (p *pluralParser) or() (pluralNode, error) {
	left, err := p.and()
	if err != nil {
		return nil, err
	}
	for {
		if !p.consume("||") {
			return left, nil
		}
		right, err := p.and()
		if err != nil {
			return nil, err
		}
		left = pluralBinary{op: "||", left: left, right: right}
	}
}

func (p *pluralParser) and() (pluralNode, error) {
	left, err := p.compare()
	if err != nil {
		return nil, err
	}
	for {
		if !p.consume("&&") {
			return left, nil
		}
		right, err := p.compare()
		if err != nil {
			return nil, err
		}
		left = pluralBinary{op: "&&", left: left, right: right}
	}
}

func (p *pluralParser) compare() (pluralNode, error) {
	left, err := p.add()
	if err != nil {
		return nil, err
	}
	for _, op := range []string{"==", "!=", "<=", ">=", "<", ">"} {
		if p.consume(op) {
			right, err := p.add()
			if err != nil {
				return nil, err
			}
			return pluralBinary{op: op, left: left, right: right}, nil
		}
	}
	return left, nil
}

func (p *pluralParser) add() (pluralNode, error) {
	left, err := p.mul()
	if err != nil {
		return nil, err
	}
	for {
		if p.consume("+") {
			right, err := p.mul()
			if err != nil {
				return nil, err
			}
			left = pluralBinary{op: "+", left: left, right: right}
			continue
		}
		if p.consume("-") {
			right, err := p.mul()
			if err != nil {
				return nil, err
			}
			left = pluralBinary{op: "-", left: left, right: right}
			continue
		}
		return left, nil
	}
}

func (p *pluralParser) mul() (pluralNode, error) {
	left, err := p.unary()
	if err != nil {
		return nil, err
	}
	for {
		if p.consume("*") {
			right, err := p.unary()
			if err != nil {
				return nil, err
			}
			left = pluralBinary{op: "*", left: left, right: right}
			continue
		}
		if p.consume("/") {
			right, err := p.unary()
			if err != nil {
				return nil, err
			}
			left = pluralBinary{op: "/", left: left, right: right}
			continue
		}
		if p.consume("%") {
			right, err := p.unary()
			if err != nil {
				return nil, err
			}
			left = pluralBinary{op: "%", left: left, right: right}
			continue
		}
		return left, nil
	}
}

func (p *pluralParser) unary() (pluralNode, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	if p.consume("!") {
		v, err := p.unary()
		if err != nil {
			return nil, err
		}
		return pluralUnary{op: "!", x: v}, nil
	}
	if p.consume("-") {
		v, err := p.unary()
		if err != nil {
			return nil, err
		}
		return pluralUnary{op: "-", x: v}, nil
	}
	return p.primary()
}

func (p *pluralParser) primary() (pluralNode, error) {
	p.skip()
	if p.consume("(") {
		v, err := p.ternary()
		if err != nil {
			return nil, err
		}
		if !p.consume(")") {
			return nil, fmt.Errorf("missing ')'")
		}
		return v, nil
	}
	if p.pos < len(p.s) && p.s[p.pos] == 'n' {
		p.pos++
		if err := p.bump(); err != nil {
			return nil, err
		}
		return pluralVar{}, nil
	}
	start := p.pos
	for p.pos < len(p.s) && unicode.IsDigit(rune(p.s[p.pos])) {
		p.pos++
	}
	if start == p.pos {
		return nil, fmt.Errorf("expected primary")
	}
	if err := p.bump(); err != nil {
		return nil, err
	}
	n, err := strconv.ParseInt(p.s[start:p.pos], 10, 64)
	if err != nil {
		return nil, err
	}
	return pluralConst(n), nil
}

func (p *pluralParser) consume(token string) bool {
	p.skip()
	if strings.HasPrefix(p.s[p.pos:], token) {
		if err := p.bump(); err != nil {
			p.err = err
			return false
		}
		p.pos += len(token)
		return true
	}
	return false
}

func (p *pluralParser) skip() {
	for p.pos < len(p.s) && unicode.IsSpace(rune(p.s[p.pos])) {
		p.pos++
	}
}

func (p *pluralParser) bump() error {
	p.tokens++
	if p.tokens > maxPluralTokens {
		err := fmt.Errorf("plural expression too complex")
		p.err = err
		return err
	}
	return nil
}

func (p *pluralParser) enter() error {
	p.depth++
	if p.depth > maxPluralDepth {
		return fmt.Errorf("plural expression too deep")
	}
	return nil
}

func (p *pluralParser) leave() {
	p.depth--
}

func boolInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}
