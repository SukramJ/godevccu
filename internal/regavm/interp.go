// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package regavm

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxLoopIterations bounds every loop. A script that would spin
// forever fails instead of hanging the request it arrived on — the
// simulator answers an HTTP call, and a wedged handler is worse than an
// error.
const maxLoopIterations = 1_000_000

// Interpreter runs a parsed script against an object model.
type Interpreter struct {
	// Root resolves the top-level namespaces: dom, system, interfaces
	// and the like.
	Root Root
	// Now supplies the clock, so tests can pin timestamps.
	Now func() time.Time
	// Exec answers system.Exec() calls. Returning ok=false makes the
	// call report failure to the script, which is what a CCU does for a
	// command that is not available.
	Exec func(command string) (output string, ok bool)
}

// Root is the environment a script starts from.
type Root interface {
	// Dom returns the ReGa object model root.
	Dom() Dom
	// Interfaces resolves an interface by name, for interfaces.Get().
	Interfaces(name string) Object
	// Serial reports the central's serial, for system.GetVar.
	Serial() string
}

// Dom is the object model a script navigates.
type Dom interface {
	// GetObject resolves by id, address or name.
	GetObject(key string) Object
	// Collection resolves a well-known collection such as ID_DEVICES.
	Collection(name string) Object
}

// Object is one node of the object model. Methods a script calls that
// an implementation does not support return the zero Value, which
// renders as an empty string — the same thing a CCU reports for an
// attribute an object does not carry.
type Object interface {
	// Name is the object's display name.
	Name() string
	// Call invokes a method with the already-evaluated arguments.
	Call(method string, args []Value) (Value, error)
}

// Result is the outcome of a run.
type Result struct {
	// Output is everything the script wrote.
	Output string
}

// scope holds the variables of a run. ReGa has no block scoping — a
// variable declared inside a loop stays visible after it — so one flat
// map per run is faithful.
type scope struct {
	vars map[string]Value
	out  strings.Builder
}

// Run executes a script and returns what it wrote.
//
// A parse error or an unsupported construct is reported rather than
// swallowed: a simulator that answers a script it did not understand
// with an empty success is the failure mode this package exists to
// avoid.
func (in *Interpreter) Run(src string) (Result, error) {
	program, err := parse(src)
	if err != nil {
		return Result{}, err
	}
	sc := &scope{vars: make(map[string]Value)}
	for _, s := range program {
		if err := in.execute(s, sc); err != nil {
			return Result{Output: sc.out.String()}, err
		}
	}
	return Result{Output: sc.out.String()}, nil
}

func (in *Interpreter) execute(s stmt, sc *scope) error {
	switch node := s.(type) {
	case *declStmt:
		value := Value{}
		if node.value != nil {
			v, err := in.eval(node.value, sc)
			if err != nil {
				return err
			}
			value = v
		}
		sc.vars[node.name] = value
		return nil

	case *assignStmt:
		v, err := in.eval(node.value, sc)
		if err != nil {
			return err
		}
		sc.vars[node.name] = v
		return nil

	case *exprStmt:
		_, err := in.eval(node.value, sc)
		return err

	case *ifStmt:
		cond, err := in.eval(node.cond, sc)
		if err != nil {
			return err
		}
		branch := node.els
		if cond.Bool() {
			branch = node.then
		}
		return in.executeAll(branch, sc)

	case *foreachStmt:
		list, err := in.eval(node.list, sc)
		if err != nil {
			return err
		}
		for i, item := range list.List() {
			if i >= maxLoopIterations {
				return fmt.Errorf("regavm: foreach exceeded %d iterations", maxLoopIterations)
			}
			sc.vars[node.variable] = item
			if err := in.executeAll(node.body, sc); err != nil {
				return err
			}
		}
		return nil

	case *whileStmt:
		for i := 0; ; i++ {
			if i >= maxLoopIterations {
				return fmt.Errorf("regavm: while exceeded %d iterations", maxLoopIterations)
			}
			cond, err := in.eval(node.cond, sc)
			if err != nil {
				return err
			}
			if !cond.Bool() {
				return nil
			}
			if err := in.executeAll(node.body, sc); err != nil {
				return err
			}
		}

	default:
		return fmt.Errorf("regavm: unsupported statement %T", s)
	}
}

func (in *Interpreter) executeAll(body []stmt, sc *scope) error {
	for _, s := range body {
		if err := in.execute(s, sc); err != nil {
			return err
		}
	}
	return nil
}

func (in *Interpreter) eval(e expr, sc *scope) (Value, error) {
	switch node := e.(type) {
	case *literalExpr:
		return node.value, nil

	case *identExpr:
		if v, ok := sc.vars[node.name]; ok {
			return v, nil
		}
		// Undeclared identifiers are namespace roots (dom, system) or
		// collection constants (ID_DEVICES); both resolve lazily.
		return stringValue(node.name), nil

	case *refExpr:
		// The reference itself carries the name; the callee writes back
		// through it.
		return stringValue("&" + node.name), nil

	case *unaryExpr:
		operand, err := in.eval(node.operand, sc)
		if err != nil {
			return Value{}, err
		}
		if node.op == "!" {
			return boolValue(!operand.Bool()), nil
		}
		return numberValue(-operand.Number()), nil

	case *binaryExpr:
		return in.evalBinary(node, sc)

	case *callExpr:
		return in.evalCall(node, sc)

	case *memberExpr:
		return in.evalMember(node, sc)

	default:
		return Value{}, fmt.Errorf("regavm: unsupported expression %T", e)
	}
}

func (in *Interpreter) evalBinary(node *binaryExpr, sc *scope) (Value, error) {
	// Short-circuit before evaluating the right side.
	left, err := in.eval(node.left, sc)
	if err != nil {
		return Value{}, err
	}
	switch node.op {
	case "&&":
		if !left.Bool() {
			return boolValue(false), nil
		}
	case "||":
		if left.Bool() {
			return boolValue(true), nil
		}
	}

	right, err := in.eval(node.right, sc)
	if err != nil {
		return Value{}, err
	}

	switch node.op {
	case "&&", "||":
		return boolValue(right.Bool()), nil
	case "#":
		return stringValue(left.String() + right.String()), nil
	case "==":
		return boolValue(left.equals(right)), nil
	case "!=":
		return boolValue(!left.equals(right)), nil
	case "<":
		return boolValue(left.Number() < right.Number()), nil
	case ">":
		return boolValue(left.Number() > right.Number()), nil
	case "<=":
		return boolValue(left.Number() <= right.Number()), nil
	case ">=":
		return boolValue(left.Number() >= right.Number()), nil
	case "+":
		// "+" concatenates when either side is a non-numeric string,
		// matching how ReGa treats mixed operands.
		if !left.numeric() || !right.numeric() {
			return stringValue(left.String() + right.String()), nil
		}
		return numberValue(left.Number() + right.Number()), nil
	case "-":
		return numberValue(left.Number() - right.Number()), nil
	case "*":
		return numberValue(left.Number() * right.Number()), nil
	case "/":
		if right.Number() == 0 {
			return numberValue(0), nil
		}
		return numberValue(left.Number() / right.Number()), nil
	case "%":
		if right.Number() == 0 {
			return numberValue(0), nil
		}
		return numberValue(math.Mod(left.Number(), right.Number())), nil
	case "&":
		// Bitwise AND, used to test OPERATIONS bits.
		return numberValue(float64(int64(left.Number()) & int64(right.Number()))), nil
	default:
		return Value{}, fmt.Errorf("regavm: unsupported operator %q", node.op)
	}
}

// evalCall handles the bare functions a script may call.
func (in *Interpreter) evalCall(node *callExpr, sc *scope) (Value, error) {
	args, err := in.evalArgs(node.args, sc)
	if err != nil {
		return Value{}, err
	}
	switch node.name {
	case "Write":
		for _, a := range args {
			sc.out.WriteString(a.String())
		}
		return Value{}, nil
	case "WriteLine":
		for _, a := range args {
			sc.out.WriteString(a.String())
		}
		sc.out.WriteString("\n")
		return Value{}, nil
	case "WriteURL":
		for _, a := range args {
			sc.out.WriteString(uriEncode(a.String()))
		}
		return Value{}, nil
	default:
		return Value{}, fmt.Errorf("regavm: unknown function %q", node.name)
	}
}

// evalArgs evaluates an argument list.
func (in *Interpreter) evalArgs(exprs []expr, sc *scope) ([]Value, error) {
	out := make([]Value, 0, len(exprs))
	for _, e := range exprs {
		v, err := in.eval(e, sc)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// evalMember dispatches "receiver.name(args)".
func (in *Interpreter) evalMember(node *memberExpr, sc *scope) (Value, error) {
	// The namespace roots are identifiers rather than values.
	if ident, ok := node.receiver.(*identExpr); ok {
		if _, declared := sc.vars[ident.name]; !declared {
			if handled, v, err := in.evalNamespace(ident.name, node, sc); handled {
				return v, err
			}
		}
	}

	receiver, err := in.eval(node.receiver, sc)
	if err != nil {
		return Value{}, err
	}
	args, err := in.evalArgs(node.args, sc)
	if err != nil {
		return Value{}, err
	}

	if receiver.kind == kindObject {
		if receiver.obj == nil {
			// A method on a null object yields nothing rather than
			// failing; scripts guard with "if (obj)" and rely on this.
			return Value{}, nil
		}
		return receiver.obj.Call(node.name, args)
	}
	return stringMethod(receiver, node.name, args)
}

// evalNamespace resolves the top-level namespaces. The bool reports
// whether the call was handled here.
func (in *Interpreter) evalNamespace(root string, node *memberExpr, sc *scope) (bool, Value, error) {
	switch root {
	case "dom":
		args, err := in.evalArgs(node.args, sc)
		if err != nil {
			return true, Value{}, err
		}
		return true, in.evalDom(node.name, args), nil
	case "system":
		v, err := in.evalSystem(node, sc)
		return true, v, err
	case "interfaces":
		args, err := in.evalArgs(node.args, sc)
		if err != nil {
			return true, Value{}, err
		}
		if node.name != "Get" || len(args) == 0 || in.Root == nil {
			return true, nullValue, nil
		}
		iface := in.Root.Interfaces(args[0].String())
		if iface == nil {
			return true, nullValue, nil
		}
		return true, objectValue(iface), nil
	case "localtime", "systime":
		args, err := in.evalArgs(node.args, sc)
		if err != nil {
			return true, Value{}, err
		}
		return true, in.evalTime(node.name, args), nil
	default:
		return false, Value{}, nil
	}
}

// evalDom resolves dom.GetObject(...).
func (in *Interpreter) evalDom(method string, args []Value) Value {
	if in.Root == nil || in.Root.Dom() == nil || len(args) == 0 {
		return nullValue
	}
	if method != "GetObject" {
		return nullValue
	}
	key := args[0].String()
	// The ID_* constants name collections; anything else is an object.
	if strings.HasPrefix(key, "ID_") {
		if collection := in.Root.Dom().Collection(key); collection != nil {
			return objectValue(collection)
		}
		return nullValue
	}
	if obj := in.Root.Dom().GetObject(key); obj != nil {
		return objectValue(obj)
	}
	return nullValue
}

// evalSystem handles the system namespace, including the out-parameter
// convention of system.Exec.
func (in *Interpreter) evalSystem(node *memberExpr, sc *scope) (Value, error) {
	switch node.name {
	case "Exec":
		if len(node.args) == 0 {
			return boolValue(false), nil
		}
		command, err := in.eval(node.args[0], sc)
		if err != nil {
			return Value{}, err
		}
		output, ok := "", false
		if in.Exec != nil {
			output, ok = in.Exec(command.String())
		}
		// The second argument is "&variable": the script reads the
		// command output from there.
		if len(node.args) > 1 {
			if ref, isRef := node.args[1].(*refExpr); isRef {
				sc.vars[ref.name] = stringValue(output)
			}
		}
		return boolValue(ok), nil
	case "GetVar":
		args, err := in.evalArgs(node.args, sc)
		if err != nil {
			return Value{}, err
		}
		if len(args) > 0 && strings.EqualFold(args[0].String(), "SERIALNO") && in.Root != nil {
			return stringValue(in.Root.Serial()), nil
		}
		return Value{}, nil
	case "Save", "SaveObjectModel":
		return boolValue(true), nil
	default:
		return Value{}, nil
	}
}

// evalTime handles localtime.Format and friends.
func (in *Interpreter) evalTime(method string, args []Value) Value {
	now := time.Now
	if in.Now != nil {
		now = in.Now
	}
	switch method {
	case "Format":
		layout := "%F %T"
		if len(args) > 0 {
			layout = args[0].String()
		}
		return stringValue(strftime(now(), layout))
	case "ToInteger", "ToTime":
		return numberValue(float64(now().Unix()))
	default:
		return stringValue(strftime(now(), "%F %T"))
	}
}

// strftime renders the subset of strftime verbs the scripts use.
func strftime(t time.Time, layout string) string {
	replacer := strings.NewReplacer(
		"%F", t.Format("2006-01-02"),
		"%T", t.Format("15:04:05"),
		"%Y", t.Format("2006"),
		"%m", t.Format("01"),
		"%d", t.Format("02"),
		"%H", t.Format("15"),
		"%M", t.Format("04"),
		"%S", t.Format("05"),
	)
	return replacer.Replace(layout)
}

// stringMethod implements the String methods a script calls on a
// scalar.
func stringMethod(receiver Value, method string, args []Value) (Value, error) {
	s := receiver.String()
	switch method {
	case "UriEncode":
		return stringValue(uriEncode(s)), nil
	case "Trim":
		return stringValue(strings.TrimSpace(s)), nil
	case "ToString":
		return stringValue(s), nil
	case "ToInteger":
		n, _ := strconv.Atoi(strings.TrimSpace(s))
		return numberValue(float64(n)), nil
	case "ToFloat", "ToReal":
		return numberValue(receiver.Number()), nil
	case "Length":
		return numberValue(float64(len(s))), nil
	case "Contains":
		if len(args) == 0 {
			return boolValue(false), nil
		}
		return boolValue(strings.Contains(s, args[0].String())), nil
	case "Find":
		if len(args) == 0 {
			return numberValue(-1), nil
		}
		return numberValue(float64(strings.Index(s, args[0].String()))), nil
	case "Substr":
		return stringValue(substr(s, args)), nil
	case "Replace":
		if len(args) < 2 {
			return stringValue(s), nil
		}
		return stringValue(strings.ReplaceAll(s, args[0].String(), args[1].String())), nil
	case "StrValueByIndex":
		return stringValue(valueByIndex(s, args)), nil
	case "ToUpper":
		return stringValue(strings.ToUpper(s)), nil
	case "ToLower":
		return stringValue(strings.ToLower(s)), nil
	case "VarType":
		return numberValue(float64(varTypeOf(receiver))), nil
	default:
		return Value{}, nil
	}
}

// substr implements String.Substr(offset[, length]).
func substr(s string, args []Value) string {
	if len(args) == 0 {
		return s
	}
	offset := int(args[0].Number())
	if offset < 0 {
		offset = 0
	}
	if offset >= len(s) {
		return ""
	}
	if len(args) < 2 {
		return s[offset:]
	}
	end := offset + int(args[1].Number())
	if end > len(s) {
		end = len(s)
	}
	if end <= offset {
		return ""
	}
	return s[offset:end]
}

// valueByIndex implements String.StrValueByIndex(separator, index).
func valueByIndex(s string, args []Value) string {
	if len(args) < 2 {
		return ""
	}
	parts := strings.Split(s, args[0].String())
	idx := int(args[1].Number())
	if idx < 0 || idx >= len(parts) {
		return ""
	}
	return parts[idx]
}

// ReGa VarType codes, as reported by Value.VarType().
const (
	varTypeInteger = 2
	varTypeFloat   = 4
	varTypeBool    = 6
	varTypeString  = 20
)

func varTypeOf(v Value) int {
	switch v.kind {
	case kindBool:
		return varTypeBool
	case kindNumber:
		if v.num == math.Trunc(v.num) {
			return varTypeInteger
		}
		return varTypeFloat
	default:
		return varTypeString
	}
}

// uriEncode mirrors ReGa's String.UriEncode(): percent-encoding with
// spaces as %20, not "+".
func uriEncode(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}
