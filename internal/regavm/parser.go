// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package regavm

import (
	"fmt"
	"strconv"
)

// The AST.
//
// Expressions and statements are kept as small interfaces the
// interpreter switches on; there is no visitor, because the language
// surface is narrow enough that a type switch stays readable.

type expr interface{ exprNode() }

type (
	// literalExpr is a string or number constant.
	literalExpr struct{ value Value }
	// identExpr reads a variable.
	identExpr struct{ name string }
	// unaryExpr is "!" or a leading "-".
	unaryExpr struct {
		op      string
		operand expr
	}
	// binaryExpr covers arithmetic, comparison, logic and the "#"
	// concatenation operator.
	binaryExpr struct {
		op          string
		left, right expr
	}
	// callExpr is a bare function call such as Write(...).
	callExpr struct {
		name string
		args []expr
	}
	// memberExpr is a method call or property read on a receiver:
	// dom.GetObject(...), oDevice.Address(), sText.Trim().
	memberExpr struct {
		receiver expr
		name     string
		args     []expr
	}
	// refExpr is "&variable", the out-parameter form system.Exec uses.
	refExpr struct{ name string }
)

func (literalExpr) exprNode() {}
func (identExpr) exprNode()   {}
func (unaryExpr) exprNode()   {}
func (binaryExpr) exprNode()  {}
func (callExpr) exprNode()    {}
func (memberExpr) exprNode()  {}
func (refExpr) exprNode()     {}

type stmt interface{ stmtNode() }

type (
	// declStmt declares a variable, optionally with an initialiser.
	declStmt struct {
		typeName string
		name     string
		value    expr
	}
	// assignStmt assigns to an existing variable.
	assignStmt struct {
		name  string
		value expr
	}
	// exprStmt evaluates an expression for its side effect.
	exprStmt struct{ value expr }
	// ifStmt is if / elseif / else.
	ifStmt struct {
		cond expr
		then []stmt
		els  []stmt
	}
	// foreachStmt iterates a list into a variable.
	foreachStmt struct {
		variable string
		list     expr
		body     []stmt
	}
	// whileStmt loops while its condition holds.
	whileStmt struct {
		cond expr
		body []stmt
	}
)

func (declStmt) stmtNode()    {}
func (assignStmt) stmtNode()  {}
func (exprStmt) stmtNode()    {}
func (ifStmt) stmtNode()      {}
func (foreachStmt) stmtNode() {}
func (whileStmt) stmtNode()   {}

// declarationTypes are the type keywords a script may open with.
var declarationTypes = map[string]bool{
	"string": true, "integer": true, "boolean": true, "bool": true,
	"object": true, "var": true, "idarray": true, "time": true,
	"real": true, "float": true,
}

// parser turns a token stream into statements.
type parser struct {
	tokens []token
	pos    int
}

// parse is the package entry point.
func parse(src string) ([]stmt, error) {
	tokens, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}
	var out []stmt
	for !p.atEOF() {
		s, err := p.statement()
		if err != nil {
			return nil, err
		}
		if s != nil {
			out = append(out, s)
		}
	}
	return out, nil
}

func (p *parser) atEOF() bool { return p.peek().kind == tokenEOF }
func (p *parser) peek() token { return p.tokens[p.pos] }
func (p *parser) next() token { t := p.tokens[p.pos]; p.pos++; return t }
func (p *parser) back()       { p.pos-- }
func (p *parser) at(text string) bool {
	t := p.peek()
	return t.text == text && t.kind != tokenString
}

// accept consumes the token when it matches.
func (p *parser) accept(text string) bool {
	if p.at(text) {
		p.pos++
		return true
	}
	return false
}

// expect consumes the token or reports where it was missing.
func (p *parser) expect(text string) error {
	if p.accept(text) {
		return nil
	}
	return fmt.Errorf("regavm: expected %q but found %s", text, p.peek())
}

// statement parses one statement, skipping stray semicolons.
func (p *parser) statement() (stmt, error) {
	for p.accept(";") {
	}
	if p.atEOF() {
		return nil, nil
	}

	t := p.peek()
	if t.kind == tokenIdent {
		switch {
		case declarationTypes[t.text] && p.declarationAhead():
			return p.declaration()
		case t.text == "if":
			return p.ifStatement()
		case t.text == "foreach":
			return p.foreachStatement()
		case t.text == "while":
			return p.whileStatement()
		}
	}
	return p.simpleStatement()
}

// declarationAhead distinguishes "string s" from a call to something
// named like a type keyword: a declaration has an identifier next.
func (p *parser) declarationAhead() bool {
	return p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].kind == tokenIdent
}

func (p *parser) declaration() (stmt, error) {
	typeName := p.next().text
	name := p.next().text
	out := &declStmt{typeName: typeName, name: name}
	if p.accept("=") {
		value, err := p.expression()
		if err != nil {
			return nil, err
		}
		out.value = value
	}
	p.accept(";")
	return out, nil
}

// simpleStatement is an assignment or a bare expression.
func (p *parser) simpleStatement() (stmt, error) {
	start := p.pos
	if p.peek().kind == tokenIdent {
		name := p.next().text
		if p.accept("=") {
			value, err := p.expression()
			if err != nil {
				return nil, err
			}
			p.accept(";")
			return &assignStmt{name: name, value: value}, nil
		}
		p.pos = start
	}
	value, err := p.expression()
	if err != nil {
		return nil, err
	}
	p.accept(";")
	return &exprStmt{value: value}, nil
}

func (p *parser) ifStatement() (stmt, error) {
	p.next() // "if"
	if err := p.expect("("); err != nil {
		return nil, err
	}
	cond, err := p.expression()
	if err != nil {
		return nil, err
	}
	if err = p.expect(")"); err != nil {
		return nil, err
	}
	then, err := p.block()
	if err != nil {
		return nil, err
	}
	out := &ifStmt{cond: cond, then: then}

	switch {
	case p.at("elseif"):
		// "elseif (...) {...}" nests as the else branch.
		nested, err := p.elseIfChain()
		if err != nil {
			return nil, err
		}
		out.els = []stmt{nested}
	case p.accept("else"):
		if p.at("if") {
			nested, err := p.ifStatement()
			if err != nil {
				return nil, err
			}
			out.els = []stmt{nested}
			break
		}
		els, err := p.block()
		if err != nil {
			return nil, err
		}
		out.els = els
	}
	return out, nil
}

// elseIfChain parses "elseif" as a nested if.
func (p *parser) elseIfChain() (stmt, error) {
	p.next() // "elseif"
	p.back()
	p.tokens[p.pos].text = "if"
	return p.ifStatement()
}

func (p *parser) foreachStatement() (stmt, error) {
	p.next() // "foreach"
	if err := p.expect("("); err != nil {
		return nil, err
	}
	variable := p.next().text
	if err := p.expect(","); err != nil {
		return nil, err
	}
	list, err := p.expression()
	if err != nil {
		return nil, err
	}
	if err = p.expect(")"); err != nil {
		return nil, err
	}
	body, err := p.block()
	if err != nil {
		return nil, err
	}
	return &foreachStmt{variable: variable, list: list, body: body}, nil
}

func (p *parser) whileStatement() (stmt, error) {
	p.next() // "while"
	if err := p.expect("("); err != nil {
		return nil, err
	}
	cond, err := p.expression()
	if err != nil {
		return nil, err
	}
	if err = p.expect(")"); err != nil {
		return nil, err
	}
	body, err := p.block()
	if err != nil {
		return nil, err
	}
	return &whileStmt{cond: cond, body: body}, nil
}

// block parses "{ ... }" or a single statement.
func (p *parser) block() ([]stmt, error) {
	if !p.accept("{") {
		s, err := p.statement()
		if err != nil {
			return nil, err
		}
		if s == nil {
			return nil, nil
		}
		return []stmt{s}, nil
	}
	var out []stmt
	for !p.at("}") {
		if p.atEOF() {
			return nil, fmt.Errorf("regavm: unterminated block")
		}
		s, err := p.statement()
		if err != nil {
			return nil, err
		}
		if s != nil {
			out = append(out, s)
		}
	}
	p.next() // "}"
	return out, nil
}

// Expression parsing, by precedence climbing.

var precedence = map[string]int{
	"||": 1, "&&": 2,
	"==": 3, "!=": 3, "<": 3, ">": 3, "<=": 3, ">=": 3,
	"#": 4,
	"+": 5, "-": 5,
	"*": 6, "/": 6, "%": 6,
	"&": 7,
}

func (p *parser) expression() (expr, error) { return p.binary(0) }

func (p *parser) binary(minPrec int) (expr, error) {
	left, err := p.unary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.kind != tokenOperator {
			return left, nil
		}
		prec, ok := precedence[t.text]
		if !ok || prec < minPrec {
			return left, nil
		}
		p.next()
		right, err := p.binary(prec + 1)
		if err != nil {
			return nil, err
		}
		left = &binaryExpr{op: t.text, left: left, right: right}
	}
}

func (p *parser) unary() (expr, error) {
	t := p.peek()
	if t.kind == tokenOperator && (t.text == "!" || t.text == "-") {
		p.next()
		operand, err := p.unary()
		if err != nil {
			return nil, err
		}
		return &unaryExpr{op: t.text, operand: operand}, nil
	}
	if t.kind == tokenOperator && t.text == "&" {
		p.next()
		name := p.next()
		if name.kind != tokenIdent {
			return nil, fmt.Errorf("regavm: expected a variable after & but found %s", name)
		}
		return &refExpr{name: name.text}, nil
	}
	return p.postfix()
}

// postfix parses a primary followed by any number of ".member(...)".
func (p *parser) postfix() (expr, error) {
	node, err := p.primary()
	if err != nil {
		return nil, err
	}
	for p.accept(".") {
		name := p.next()
		if name.kind != tokenIdent {
			return nil, fmt.Errorf("regavm: expected a member name but found %s", name)
		}
		member := &memberExpr{receiver: node, name: name.text}
		if p.accept("(") {
			args, err := p.arguments()
			if err != nil {
				return nil, err
			}
			member.args = args
		}
		node = member
	}
	return node, nil
}

func (p *parser) primary() (expr, error) {
	t := p.next()
	switch {
	case t.kind == tokenString:
		return &literalExpr{value: stringValue(t.text)}, nil
	case t.kind == tokenNumber:
		f, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			return nil, fmt.Errorf("regavm: bad number %s", t)
		}
		return &literalExpr{value: numberValue(f)}, nil
	case t.text == "(":
		inner, err := p.expression()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		return inner, nil
	case t.kind == tokenIdent:
		switch t.text {
		case "true":
			return &literalExpr{value: boolValue(true)}, nil
		case "false":
			return &literalExpr{value: boolValue(false)}, nil
		}
		if p.accept("(") {
			args, err := p.arguments()
			if err != nil {
				return nil, err
			}
			return &callExpr{name: t.text, args: args}, nil
		}
		return &identExpr{name: t.text}, nil
	default:
		return nil, fmt.Errorf("regavm: unexpected token %s", t)
	}
}

// arguments parses a comma-separated list up to the closing paren,
// which the caller has already opened.
func (p *parser) arguments() ([]expr, error) {
	var args []expr
	if p.accept(")") {
		return args, nil
	}
	for {
		arg, err := p.expression()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		if p.accept(",") {
			continue
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		return args, nil
	}
}
