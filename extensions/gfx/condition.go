package gfx

import "errors"

// condExpr is a parsed #if condition. Operands are defines; operators are !, &,
// | and parentheses. There is no comparison, no arithmetic and no const operand,
// which is exactly what the define/const split buys: #if never has to reason
// about a value.
type condExpr struct {
	op    byte   // 0 for a name, otherwise '!', '&' or '|'
	name  string // for op == 0
	terms []condExpr
}

// eval reports whether the condition holds under the module's define union. An
// undefined name is false, always, with no attempt to detect a typo: there is no
// #undef and no off-declaration, so absent is the only spelling of "flag is
// off", and erroring on an unknown name is not reachable in this grammar.
func (e condExpr) eval(defines map[string]bool) bool {
	switch e.op {
	case '!':
		return !e.terms[0].eval(defines)
	case '&':
		for _, term := range e.terms {
			if !term.eval(defines) {
				return false
			}
		}
		return true
	case '|':
		for _, term := range e.terms {
			if term.eval(defines) {
				return true
			}
		}
		return false
	}
	return defines[e.name]
}

// errMixedOperators is the one condition error worth naming, because the repair
// is not obvious from "syntax error": there is no precedence between & and |,
// so mixing them at one level does not compile rather than becoming a puzzle.
var errMixedOperators = errors.New("`&` and `|` do not mix without parentheses; write `A | (B & C)` or `(A | B) & C`")

var errEmptyCondition = errors.New("empty condition")

// parseCondition parses one #if or #elif condition.
//
// The grammar is three productions with no ambiguity to remember:
//
//	expr := term (('&' term)... | ('|' term)...)
//	term := '!' term | NAME | '(' expr ')'
//
// `!` is unary and binds to a single name or a parenthesised group. It is in the
// grammar because without it `#if !A & B` has no spelling at all - it becomes
// `#if B` wrapping a nested `#if A` / `#else`, which duplicates the B-branch
// body, and duplicated bodies drift.
func parseCondition(text string) (condExpr, error) {
	p := &condParser{text: text}
	p.skipSpace()
	if p.at >= len(p.text) {
		return condExpr{}, errEmptyCondition
	}
	expr, err := p.parseExpr()
	if err != nil {
		return condExpr{}, err
	}
	p.skipSpace()
	if p.at < len(p.text) {
		return condExpr{}, errors.New("unexpected `" + string(p.text[p.at]) + "` in condition")
	}
	return expr, nil
}

type condParser struct {
	text string
	at   int
}

func (p *condParser) skipSpace() {
	for p.at < len(p.text) && (p.text[p.at] == ' ' || p.text[p.at] == '\t') {
		p.at++
	}
}

func (p *condParser) parseExpr() (condExpr, error) {
	first, err := p.parseTerm()
	if err != nil {
		return condExpr{}, err
	}
	p.skipSpace()
	if p.at >= len(p.text) || (p.text[p.at] != '&' && p.text[p.at] != '|') {
		return first, nil
	}
	op := p.text[p.at]
	terms := []condExpr{first}
	for {
		p.skipSpace()
		if p.at >= len(p.text) || (p.text[p.at] != '&' && p.text[p.at] != '|') {
			return condExpr{op: op, terms: terms}, nil
		}
		if p.text[p.at] != op {
			return condExpr{}, errMixedOperators
		}
		p.at++
		term, err := p.parseTerm()
		if err != nil {
			return condExpr{}, err
		}
		terms = append(terms, term)
	}
}

func (p *condParser) parseTerm() (condExpr, error) {
	p.skipSpace()
	if p.at >= len(p.text) {
		return condExpr{}, errors.New("condition ends after an operator")
	}
	switch c := p.text[p.at]; {
	case c == '!':
		p.at++
		term, err := p.parseTerm()
		if err != nil {
			return condExpr{}, err
		}
		return condExpr{op: '!', terms: []condExpr{term}}, nil
	case c == '(':
		p.at++
		inner, err := p.parseExpr()
		if err != nil {
			return condExpr{}, err
		}
		p.skipSpace()
		if p.at >= len(p.text) || p.text[p.at] != ')' {
			return condExpr{}, errors.New("unclosed `(` in condition")
		}
		p.at++
		return inner, nil
	case isNameByte(c):
		start := p.at
		for p.at < len(p.text) && isNameByte(p.text[p.at]) {
			p.at++
		}
		return condExpr{name: p.text[start:p.at]}, nil
	}
	return condExpr{}, errors.New("unexpected `" + string(p.text[p.at]) + "` in condition")
}

func isNameByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}
