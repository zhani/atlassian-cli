package mirror

import (
	"fmt"
	"strings"
	"unicode"
)

// A parser for the query syntax JQL and CQL share: clauses combined with
// AND, OR, NOT and parentheses, optionally followed by ORDER BY. A clause is
// `field op value`, `field [NOT] IN (values)` or `field IS [NOT] EMPTY`;
// values are words, quoted strings or function calls.

type tokenKind int

const (
	tokWord tokenKind = iota
	tokString
	tokPunct
	tokEOF
)

type token struct {
	kind tokenKind
	text string
}

func (t token) is(word string) bool {
	return t.kind == tokWord && strings.EqualFold(t.text, word)
}

func lex(q string) ([]token, error) {
	var toks []token
	r := []rune(q)
	for i := 0; i < len(r); {
		c := r[i]
		switch {
		case unicode.IsSpace(c):
			i++
		case c == '"' || c == '\'':
			var sb strings.Builder
			j := i + 1
			for ; j < len(r) && r[j] != c; j++ {
				if r[j] == '\\' && j+1 < len(r) {
					j++
				}
				sb.WriteRune(r[j])
			}
			if j >= len(r) {
				return nil, fmt.Errorf("unterminated string starting at %d", i)
			}
			toks = append(toks, token{tokString, sb.String()})
			i = j + 1
		case c == '(' || c == ')' || c == ',':
			toks = append(toks, token{tokPunct, string(c)})
			i++
		case c == '!' || c == '<' || c == '>' || c == '=' || c == '~':
			op := string(c)
			if i+1 < len(r) && (r[i+1] == '=' || (c == '!' && r[i+1] == '~')) {
				op += string(r[i+1])
			}
			toks = append(toks, token{tokPunct, op})
			i += len([]rune(op))
		case c == '&' && i+1 < len(r) && r[i+1] == '&':
			toks = append(toks, token{tokWord, "AND"})
			i += 2
		case c == '|' && i+1 < len(r) && r[i+1] == '|':
			toks = append(toks, token{tokWord, "OR"})
			i += 2
		default:
			j := i
			for j < len(r) && !unicode.IsSpace(r[j]) && !strings.ContainsRune(`()",'=!<>~`, r[j]) {
				j++
			}
			toks = append(toks, token{tokWord, string(r[i:j])})
			i = j
		}
	}
	return append(toks, token{kind: tokEOF}), nil
}

// operand is a clause value: a literal or a function call.
type operand struct {
	text   string
	quoted bool
	fn     string // lower-case function name, "" for a literal
	args   []string
}

type node interface{}

type andNode struct{ l, r node }
type orNode struct{ l, r node }
type notNode struct{ x node }

type clause struct {
	field  string // lower-case, unquoted
	raw    string // as written
	op     string // = != ~ !~ > >= < <= in "not in" is "is not"
	values []operand
}

type sortKey struct {
	field string
	desc  bool
}

type query struct {
	where node // nil: everything
	order []sortKey
}

type parser struct {
	toks []token
	pos  int
}

func (p *parser) peek() token { return p.toks[p.pos] }
func (p *parser) next() token {
	t := p.toks[p.pos]
	if t.kind != tokEOF {
		p.pos++
	}
	return t
}

func parseQuery(q string) (*query, error) {
	toks, err := lex(q)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	out := &query{}
	if !p.peek().is("ORDER") && p.peek().kind != tokEOF {
		if out.where, err = p.or(); err != nil {
			return nil, err
		}
	}
	if p.peek().is("ORDER") {
		p.next()
		if !p.next().is("BY") {
			return nil, fmt.Errorf("expected BY after ORDER")
		}
		for {
			t := p.next()
			if t.kind != tokWord && t.kind != tokString {
				return nil, fmt.Errorf("expected a field after ORDER BY")
			}
			k := sortKey{field: strings.ToLower(t.text)}
			if p.peek().is("DESC") {
				p.next()
				k.desc = true
			} else if p.peek().is("ASC") {
				p.next()
			}
			out.order = append(out.order, k)
			if p.peek().kind == tokPunct && p.peek().text == "," {
				p.next()
				continue
			}
			break
		}
	}
	if t := p.peek(); t.kind != tokEOF {
		return nil, fmt.Errorf("unexpected %q", t.text)
	}
	return out, nil
}

func (p *parser) or() (node, error) {
	l, err := p.and()
	if err != nil {
		return nil, err
	}
	for p.peek().is("OR") {
		p.next()
		r, err := p.and()
		if err != nil {
			return nil, err
		}
		l = orNode{l, r}
	}
	return l, nil
}

func (p *parser) and() (node, error) {
	l, err := p.not()
	if err != nil {
		return nil, err
	}
	for p.peek().is("AND") {
		p.next()
		r, err := p.not()
		if err != nil {
			return nil, err
		}
		l = andNode{l, r}
	}
	return l, nil
}

func (p *parser) not() (node, error) {
	t := p.peek()
	if t.is("NOT") || (t.kind == tokPunct && t.text == "!") {
		p.next()
		x, err := p.not()
		return notNode{x}, err
	}
	if t.kind == tokPunct && t.text == "(" {
		p.next()
		x, err := p.or()
		if err != nil {
			return nil, err
		}
		if c := p.next(); c.kind != tokPunct || c.text != ")" {
			return nil, fmt.Errorf("expected )")
		}
		return x, nil
	}
	return p.clause()
}

func (p *parser) clause() (node, error) {
	f := p.next()
	if f.kind != tokWord && f.kind != tokString {
		if f.kind == tokEOF {
			return nil, fmt.Errorf("unexpected end of query")
		}
		return nil, fmt.Errorf("expected a field, got %q", f.text)
	}
	c := &clause{field: strings.ToLower(strings.TrimSpace(f.text)), raw: f.text}
	op := p.next()
	switch {
	case op.is("IS"):
		c.op = "is"
		if p.peek().is("NOT") {
			p.next()
			c.op = "is not"
		}
		if v := p.next(); !v.is("EMPTY") && !v.is("NULL") {
			return nil, fmt.Errorf("expected EMPTY after IS")
		}
		return c, nil
	case op.is("NOT"):
		if !p.next().is("IN") {
			return nil, fmt.Errorf("expected IN after NOT")
		}
		c.op = "not in"
	case op.is("IN"):
		c.op = "in"
	case op.is("WAS") || op.is("CHANGED"):
		return nil, fmt.Errorf("%s is not supported by the offline mirror", strings.ToUpper(op.text))
	case op.kind == tokPunct && op.text != "(" && op.text != ")" && op.text != ",":
		c.op = op.text
	default:
		return nil, fmt.Errorf("expected an operator after %s", f.text)
	}
	if t := p.peek(); t.kind == tokPunct && t.text == "(" {
		p.next()
		for {
			v, err := p.operand()
			if err != nil {
				return nil, err
			}
			c.values = append(c.values, v)
			t := p.next()
			if t.kind == tokPunct && t.text == ")" {
				break
			}
			if t.kind != tokPunct || t.text != "," {
				return nil, fmt.Errorf("expected , or ) in list")
			}
		}
		return c, nil
	}
	v, err := p.operand()
	if err != nil {
		return nil, err
	}
	if !v.quoted && v.fn == "" && (strings.EqualFold(v.text, "EMPTY") || strings.EqualFold(v.text, "NULL")) {
		switch c.op {
		case "=":
			c.op = "is"
			return c, nil
		case "!=":
			c.op = "is not"
			return c, nil
		}
	}
	c.values = []operand{v}
	return c, nil
}

func (p *parser) operand() (operand, error) {
	t := p.next()
	switch t.kind {
	case tokString:
		return operand{text: t.text, quoted: true}, nil
	case tokWord:
	default:
		return operand{}, fmt.Errorf("expected a value, got %q", t.text)
	}
	if n := p.peek(); n.kind == tokPunct && n.text == "(" {
		p.next()
		o := operand{text: t.text, fn: strings.ToLower(t.text)}
		if c := p.peek(); c.kind == tokPunct && c.text == ")" {
			p.next()
			return o, nil
		}
		for {
			a := p.next()
			if a.kind != tokWord && a.kind != tokString {
				return operand{}, fmt.Errorf("bad argument to %s()", t.text)
			}
			o.args = append(o.args, a.text)
			s := p.next()
			if s.kind == tokPunct && s.text == ")" {
				return o, nil
			}
			if s.kind != tokPunct || s.text != "," {
				return operand{}, fmt.Errorf("expected , or ) in %s()", t.text)
			}
		}
	}
	return operand{text: t.text}, nil
}

// eval evaluates a parsed condition with match deciding single clauses.
func eval(n node, match func(*clause) (bool, error)) (bool, error) {
	switch n := n.(type) {
	case nil:
		return true, nil
	case andNode:
		l, err := eval(n.l, match)
		if err != nil || !l {
			return false, err
		}
		return eval(n.r, match)
	case orNode:
		l, err := eval(n.l, match)
		if err != nil || l {
			return l, err
		}
		return eval(n.r, match)
	case notNode:
		x, err := eval(n.x, match)
		return !x, err
	case *clause:
		return match(n)
	}
	return false, fmt.Errorf("bad query node %T", n)
}

// textMatches implements `~`: a quoted phrase is a substring search,
// otherwise every word (a trailing * is a prefix wildcard) must occur.
func textMatches(haystack, needle string) bool {
	h := strings.ToLower(haystack)
	n := strings.ToLower(strings.TrimSpace(needle))
	if len(n) >= 2 && n[0] == '"' && n[len(n)-1] == '"' {
		return strings.Contains(h, strings.Trim(n, `"`))
	}
	words := strings.FieldsFunc(n, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '*' && r != '?' && r != '_'
	})
	if len(words) == 0 {
		return true
	}
	for _, w := range words {
		w = strings.Trim(w, "*?")
		if w != "" && !strings.Contains(h, w) {
			return false
		}
	}
	return true
}
