package shader

import "errors"

// errMixedOperators is the one condition error worth naming, because its
// repair is not obvious from a syntax error: & and | have no precedence
// between them, so mixing them at one level does not parse.
var errMixedOperators = errors.New("`&` and `|` do not mix without parentheses; write `A | (B & C)` or `(A | B) & C`")

// errEmptyCondition is an #if or #elif with nothing after it.
var errEmptyCondition = errors.New("empty condition")

// condition is a parsed #if or #elif condition. Its operands are defines, its
// operators !, &, | and parentheses. It has no comparison and no const
// operand: that is what keeping defines and consts apart buys, as #if never
// reasons about a value.
type condition struct {
	operator byte   // 0 for a name, otherwise '!', '&' or '|'
	name     string // for operator 0
	terms    []condition
}

// parseCondition parses one #if or #elif condition, by a grammar of two
// productions:
//
//	expression := term (('&' term)... | ('|' term)...)
//	term       := '!' term | NAME | '(' expression ')'
//
// ! binds to one name or a parenthesised group. Without it `#if !A & B` has
// no spelling but a B branch around a nested #if A and #else, which repeats
// the body, and repeated bodies drift.
func parseCondition(text string) (condition, error) {
	parser := &conditionParser{text: text}
	parser.skipSpace()
	if parser.at >= len(parser.text) {
		return condition{}, errEmptyCondition
	}
	parsed, err := parser.parseExpression()
	if err != nil {
		return condition{}, err
	}
	parser.skipSpace()
	if parser.at < len(parser.text) {
		return condition{}, errors.New("unexpected `" + string(parser.text[parser.at]) + "` in condition")
	}
	return parsed, nil
}

// evaluate reports whether the condition holds over defines. An undefined
// name is false, always: there is no #undef, so absent is the only spelling of
// a flag that is off, and a typo cannot be told from one.
func (c condition) evaluate(defines map[string]bool) bool {
	switch c.operator {
	case '!':
		return !c.terms[0].evaluate(defines)
	case '&':
		for _, term := range c.terms {
			if !term.evaluate(defines) {
				return false
			}
		}
		return true
	case '|':
		for _, term := range c.terms {
			if term.evaluate(defines) {
				return true
			}
		}
		return false
	}
	return defines[c.name]
}

// conditionParser walks the text of one condition; at is the next unread
// byte.
type conditionParser struct {
	text string
	at   int
}

func (p *conditionParser) skipSpace() {
	for p.at < len(p.text) && (p.text[p.at] == ' ' || p.text[p.at] == '\t') {
		p.at++
	}
}

func (p *conditionParser) parseExpression() (condition, error) {
	first, err := p.parseTerm()
	if err != nil {
		return condition{}, err
	}
	p.skipSpace()
	if p.at >= len(p.text) || (p.text[p.at] != '&' && p.text[p.at] != '|') {
		return first, nil
	}
	operator := p.text[p.at]
	terms := []condition{first}
	for {
		p.skipSpace()
		if p.at >= len(p.text) || (p.text[p.at] != '&' && p.text[p.at] != '|') {
			return condition{operator: operator, terms: terms}, nil
		}
		if p.text[p.at] != operator {
			return condition{}, errMixedOperators
		}
		p.at++
		term, err := p.parseTerm()
		if err != nil {
			return condition{}, err
		}
		terms = append(terms, term)
	}
}

func (p *conditionParser) parseTerm() (condition, error) {
	p.skipSpace()
	if p.at >= len(p.text) {
		return condition{}, errors.New("condition ends after an operator")
	}
	switch character := p.text[p.at]; {
	case character == '!':
		p.at++
		term, err := p.parseTerm()
		if err != nil {
			return condition{}, err
		}
		return condition{operator: '!', terms: []condition{term}}, nil
	case character == '(':
		p.at++
		inner, err := p.parseExpression()
		if err != nil {
			return condition{}, err
		}
		p.skipSpace()
		if p.at >= len(p.text) || p.text[p.at] != ')' {
			return condition{}, errors.New("unclosed `(` in condition")
		}
		p.at++
		return inner, nil
	case isNameByte(character):
		start := p.at
		for p.at < len(p.text) && isNameByte(p.text[p.at]) {
			p.at++
		}
		return condition{name: p.text[start:p.at]}, nil
	}
	return condition{}, errors.New("unexpected `" + string(p.text[p.at]) + "` in condition")
}
