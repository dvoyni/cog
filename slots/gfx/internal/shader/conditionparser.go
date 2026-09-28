package shader

import "errors"

// conditionParser walks the text of one condition for parseCondition; at is the
// next unread byte.
type conditionParser struct {
	text string
	at   int
}

func (p *conditionParser) skipSpace() {
	for p.at < len(p.text) && (p.text[p.at] == ' ' || p.text[p.at] == '\t') {
		p.at++
	}
}

func (p *conditionParser) parseExpr() (conditionExpression, error) {
	first, err := p.parseTerm()
	if err != nil {
		return conditionExpression{}, err
	}
	p.skipSpace()
	if p.at >= len(p.text) || (p.text[p.at] != '&' && p.text[p.at] != '|') {
		return first, nil
	}
	op := p.text[p.at]
	terms := []conditionExpression{first}
	for {
		p.skipSpace()
		if p.at >= len(p.text) || (p.text[p.at] != '&' && p.text[p.at] != '|') {
			return conditionExpression{op: op, terms: terms}, nil
		}
		if p.text[p.at] != op {
			return conditionExpression{}, errMixedOperators
		}
		p.at++
		term, err := p.parseTerm()
		if err != nil {
			return conditionExpression{}, err
		}
		terms = append(terms, term)
	}
}

func (p *conditionParser) parseTerm() (conditionExpression, error) {
	p.skipSpace()
	if p.at >= len(p.text) {
		return conditionExpression{}, errors.New("condition ends after an operator")
	}
	switch c := p.text[p.at]; {
	case c == '!':
		p.at++
		term, err := p.parseTerm()
		if err != nil {
			return conditionExpression{}, err
		}
		return conditionExpression{op: '!', terms: []conditionExpression{term}}, nil
	case c == '(':
		p.at++
		inner, err := p.parseExpr()
		if err != nil {
			return conditionExpression{}, err
		}
		p.skipSpace()
		if p.at >= len(p.text) || p.text[p.at] != ')' {
			return conditionExpression{}, errors.New("unclosed `(` in condition")
		}
		p.at++
		return inner, nil
	case isNameByte(c):
		start := p.at
		for p.at < len(p.text) && isNameByte(p.text[p.at]) {
			p.at++
		}
		return conditionExpression{name: p.text[start:p.at]}, nil
	}
	return conditionExpression{}, errors.New("unexpected `" + string(p.text[p.at]) + "` in condition")
}

func isNameByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}
