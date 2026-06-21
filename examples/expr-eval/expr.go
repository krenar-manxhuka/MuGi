// Package expr evaluates arithmetic expressions over float64.
//
// Supported syntax:
//   - Decimal numbers: 1, 1.5, .5, 2e3, 1.5e-2
//   - Binary operators: + - * / with standard precedence and left-associativity
//   - Parentheses for grouping
//   - Unary minus: -3, -(2+1)
//
// Unary plus is NOT supported; a '+' in unary position is a syntax error.
// Double unary minus (e.g. "--3") is also a syntax error.
package expr

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// tokenKind identifies the type of a lexical token.
type tokenKind int

const (
	tokNumber tokenKind = iota
	tokPlus
	tokMinus
	tokStar
	tokSlash
	tokLParen
	tokRParen
	tokEOF
)

// token is a single lexical unit produced by the lexer.
type token struct {
	kind  tokenKind
	value float64 // valid only when kind == tokNumber
}

// lex scans the input string and returns a slice of tokens terminated by
// tokEOF, or an error if an unrecognised character is encountered.
func lex(input string) ([]token, error) {
	tokens := make([]token, 0, 16)
	i := 0
	runes := []rune(input)
	n := len(runes)

	for i < n {
		ch := runes[i]

		// Skip whitespace.
		if unicode.IsSpace(ch) {
			i++
			continue
		}

		switch ch {
		case '+':
			tokens = append(tokens, token{kind: tokPlus})
			i++
		case '-':
			tokens = append(tokens, token{kind: tokMinus})
			i++
		case '*':
			tokens = append(tokens, token{kind: tokStar})
			i++
		case '/':
			tokens = append(tokens, token{kind: tokSlash})
			i++
		case '(':
			tokens = append(tokens, token{kind: tokLParen})
			i++
		case ')':
			tokens = append(tokens, token{kind: tokRParen})
			i++
		default:
			// Try to scan a number: digits, optional dot, optional exponent.
			// Valid forms: 123  1.5  .5  2e3  1.5e-2
			if unicode.IsDigit(ch) || ch == '.' {
				start := i
				// Consume leading digits.
				for i < n && unicode.IsDigit(runes[i]) {
					i++
				}
				// Optional fractional part.
				if i < n && runes[i] == '.' {
					i++
					for i < n && unicode.IsDigit(runes[i]) {
						i++
					}
				}
				// Optional exponent part: e or E, optional sign, digits.
				if i < n && (runes[i] == 'e' || runes[i] == 'E') {
					i++
					if i < n && (runes[i] == '+' || runes[i] == '-') {
						i++
					}
					for i < n && unicode.IsDigit(runes[i]) {
						i++
					}
				}
				numStr := string(runes[start:i])
				val, err := strconv.ParseFloat(numStr, 64)
				if err != nil {
					return nil, fmt.Errorf("invalid number literal %q: %w", numStr, err)
				}
				tokens = append(tokens, token{kind: tokNumber, value: val})
			} else {
				return nil, fmt.Errorf("unknown character %q at position %d", ch, i)
			}
		}
	}

	tokens = append(tokens, token{kind: tokEOF})
	return tokens, nil
}

// parser holds the token stream and a cursor position.
type parser struct {
	tokens []token
	pos    int
}

// peek returns the current token without consuming it.
func (p *parser) peek() token {
	return p.tokens[p.pos]
}

// consume advances the cursor and returns the token that was current.
func (p *parser) consume() token {
	t := p.tokens[p.pos]
	p.pos++
	return t
}

// parseExpr handles addition and subtraction (lowest precedence).
//
//	expr = term { ('+' | '-') term }
func (p *parser) parseExpr() (float64, error) {
	left, err := p.parseTerm()
	if err != nil {
		return 0, err
	}

	for {
		op := p.peek()
		if op.kind != tokPlus && op.kind != tokMinus {
			break
		}
		p.consume()
		right, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		if op.kind == tokPlus {
			left += right
		} else {
			left -= right
		}
	}
	return left, nil
}

// parseTerm handles multiplication and division.
//
//	term = unary { ('*' | '/') unary }
func (p *parser) parseTerm() (float64, error) {
	left, err := p.parseUnary()
	if err != nil {
		return 0, err
	}

	for {
		op := p.peek()
		if op.kind != tokStar && op.kind != tokSlash {
			break
		}
		p.consume()
		right, err := p.parseUnary()
		if err != nil {
			return 0, err
		}
		if op.kind == tokStar {
			left *= right
		} else {
			// Explicit division-by-zero check; Go float64 would produce ±Inf.
			if right == 0.0 {
				return 0, fmt.Errorf("division by zero")
			}
			left /= right
		}
	}
	return left, nil
}

// parseUnary handles a single unary minus.
//
// Only one level of unary minus is permitted. Unary plus is rejected as a
// syntax error, which ensures that "1 + + 2" is an error as required by spec.
//
//	unary = '-' primary | primary
func (p *parser) parseUnary() (float64, error) {
	if p.peek().kind == tokMinus {
		p.consume()
		// Delegate directly to primary — no recursive unary allowed.
		// This means "--3" is a syntax error.
		val, err := p.parsePrimary()
		if err != nil {
			return 0, err
		}
		return -val, nil
	}
	if p.peek().kind == tokPlus {
		// Unary plus is not supported.
		return 0, fmt.Errorf("unexpected '+' in unary position")
	}
	return p.parsePrimary()
}

// parsePrimary handles numbers and parenthesised sub-expressions.
//
//	primary = NUMBER | '(' expr ')'
func (p *parser) parsePrimary() (float64, error) {
	t := p.peek()
	switch t.kind {
	case tokNumber:
		p.consume()
		return t.value, nil
	case tokLParen:
		p.consume() // consume '('
		val, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		if p.peek().kind != tokRParen {
			return 0, fmt.Errorf("mismatched parentheses: expected ')'")
		}
		p.consume() // consume ')'
		return val, nil
	case tokRParen:
		return 0, fmt.Errorf("mismatched parentheses: unexpected ')'")
	case tokEOF:
		return 0, fmt.Errorf("unexpected end of expression")
	default:
		return 0, fmt.Errorf("unexpected token in expression")
	}
}

// Eval parses and evaluates an arithmetic expression string, returning the
// result as a float64. It returns a non-nil error for empty input,
// mismatched parentheses, unknown characters, division by zero, trailing
// operators, or two operators in a row.
func Eval(expression string) (float64, error) {
	// Reject empty or whitespace-only input.
	if strings.TrimSpace(expression) == "" {
		return 0, fmt.Errorf("empty expression")
	}

	tokens, err := lex(expression)
	if err != nil {
		return 0, err
	}

	p := &parser{tokens: tokens}
	result, err := p.parseExpr()
	if err != nil {
		return 0, err
	}

	// Ensure the entire input was consumed.
	if p.peek().kind != tokEOF {
		return 0, fmt.Errorf("unexpected token after expression")
	}

	return result, nil
}
