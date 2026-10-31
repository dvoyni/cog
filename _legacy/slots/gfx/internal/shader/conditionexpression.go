package shader

import "errors"

// conditionExpression is a parsed #if condition. Operands are defines;
// operators are !, &, | and parentheses. There is no comparison, no arithmetic
// and no const operand, which is exactly what the define/const split buys: #if
// never has to reason about a value.
type conditionExpression struct {
	op    byte   // 0 for a name, otherwise '!', '&' or '|'
	name  string // for op == 0
	terms []conditionExpression
}

// eval reports whether the condition holds under the module's define union. An
// undefined name is false, always, with no attempt to detect a typo: there is no
// #undef and no off-declaration, so absent is the only spelling of "flag is
// off", and erroring on an unknown name is not reachable in this grammar.
func (e conditionExpression) eval(defines map[string]bool) bool {
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
func parseCondition(text string) (conditionExpression, error) {
	p := &conditionParser{text: text}
	p.skipSpace()
	if p.at >= len(p.text) {
		return conditionExpression{}, errEmptyCondition
	}
	expr, err := p.parseExpr()
	if err != nil {
		return conditionExpression{}, err
	}
	p.skipSpace()
	if p.at < len(p.text) {
		return conditionExpression{}, errors.New("unexpected `" + string(p.text[p.at]) + "` in condition")
	}
	return expr, nil
}
