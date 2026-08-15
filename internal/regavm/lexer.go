// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Package regavm interprets the subset of HomeMatic Script (ReGa) that
// clients actually send to a CCU.
//
// The pattern-matching engine in internal/rega answers known scripts by
// recognising them; this package runs them. That matters for clients
// whose scripts the pattern engine has never seen — ccu-jack sends its
// own, and any user-written script through ReGa.runScript is a blank
// for the matcher.
//
// Scope is deliberately the observed language, not the full one: the
// declarations, control flow, operators and object methods that appear
// in the scripts under internal/rega/testdata/scripts and in the
// clients around this repository. Anything outside that surfaces as an
// error rather than a wrong answer — see [Interpreter.Run].
package regavm

import (
	"fmt"
	"strings"
	"unicode"
)

// tokenKind classifies a lexeme.
type tokenKind int

const (
	tokenEOF tokenKind = iota
	tokenIdent
	tokenNumber
	tokenString
	tokenOperator
	tokenPunct
)

// token is one lexeme with its source offset, used for error messages.
type token struct {
	kind tokenKind
	text string
	line int
}

func (t token) String() string { return fmt.Sprintf("%q (line %d)", t.text, t.line) }

// operators of the language, longest first so that ">=" wins over ">".
var operators = []string{
	"==", "!=", "<=", ">=", "&&", "||", "<", ">", "=", "+", "-", "*", "/", "%", "#", "!", "&",
}

// lex turns source into a token stream.
//
// ReGa comments come in two spellings: "!#" introduces the header
// comments a client ships, and a bare "!" comments out the rest of the
// line inside a script body. Both run to the end of the line.
func lex(src string) ([]token, error) {
	var out []token
	line := 1
	i := 0
	for i < len(src) {
		c := src[i]

		switch c {
		case '\n':
			line++
			i++
			continue
		case ' ', '\t', '\r':
			i++
			continue
		}

		// Comments: "!#" and "!" — but "!=" is an operator, and a "!"
		// directly before an expression is negation. Treat "!" as a
		// comment only when it starts a line or is followed by "#".
		if c == '!' {
			if i+1 < len(src) && src[i+1] == '=' {
				out = append(out, token{kind: tokenOperator, text: "!=", line: line})
				i += 2
				continue
			}
			if i+1 < len(src) && src[i+1] == '#' || startsLine(src, i) {
				for i < len(src) && src[i] != '\n' {
					i++
				}
				continue
			}
			out = append(out, token{kind: tokenOperator, text: "!", line: line})
			i++
			continue
		}

		switch {
		case c == '"' || c == '\'':
			text, next, err := lexString(src, i, line)
			if err != nil {
				return nil, err
			}
			out = append(out, token{kind: tokenString, text: text, line: line})
			i = next
		case unicode.IsDigit(rune(c)):
			start := i
			for i < len(src) && (unicode.IsDigit(rune(src[i])) || src[i] == '.') {
				i++
			}
			out = append(out, token{kind: tokenNumber, text: src[start:i], line: line})
		case isIdentStart(c):
			start := i
			for i < len(src) && isIdentPart(src[i]) {
				i++
			}
			out = append(out, token{kind: tokenIdent, text: src[start:i], line: line})
		case strings.ContainsRune("(){};,.", rune(c)):
			out = append(out, token{kind: tokenPunct, text: string(c), line: line})
			i++
		default:
			matched := false
			for _, op := range operators {
				if strings.HasPrefix(src[i:], op) {
					out = append(out, token{kind: tokenOperator, text: op, line: line})
					i += len(op)
					matched = true
					break
				}
			}
			if !matched {
				return nil, fmt.Errorf("regavm: unexpected character %q on line %d", string(c), line)
			}
		}
	}
	out = append(out, token{kind: tokenEOF, line: line})
	return out, nil
}

// lexString reads a single- or double-quoted literal, honouring
// backslash escapes.
func lexString(src string, start, line int) (text string, next int, err error) {
	quote := src[start]
	var b strings.Builder
	i := start + 1
	for i < len(src) {
		switch src[i] {
		case '\\':
			if i+1 >= len(src) {
				return "", 0, fmt.Errorf("regavm: unterminated escape on line %d", line)
			}
			b.WriteByte(unescape(src[i+1]))
			i += 2
		case quote:
			return b.String(), i + 1, nil
		default:
			b.WriteByte(src[i])
			i++
		}
	}
	return "", 0, fmt.Errorf("regavm: unterminated string on line %d", line)
}

// unescape maps an escaped character to its value.
func unescape(c byte) byte {
	switch c {
	case 'n':
		return '\n'
	case 't':
		return '\t'
	case 'r':
		return '\r'
	default:
		return c
	}
}

// startsLine reports whether only whitespace precedes offset i on its
// line, which is what makes a bare "!" a comment rather than negation.
func startsLine(src string, i int) bool {
	for j := i - 1; j >= 0; j-- {
		switch src[j] {
		case '\n':
			return true
		case ' ', '\t', '\r':
			continue
		default:
			return false
		}
	}
	return true
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || unicode.IsLetter(rune(c))
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || unicode.IsDigit(rune(c))
}
