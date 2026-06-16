// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package lib

import (
	"fmt"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/common/operators"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/google/cel-go/interpreter"
	"github.com/google/cel-go/parser"
)

// Emitter is the callback interface used by the emit macro to publish events
// during CEL evaluation.
type Emitter interface {
	Emit(value, cursor any) error
}

const (
	emitSentinel       = "@emit"
	emitBodySentinel   = "@emit_body"
	emitCursorSentinel = "@emit_cursor"
	emitAccuVar        = "@emit_result"
)

// Emit returns a cel.EnvOption that configures the emit macro. The factory
// function is called at eval time to obtain the session-scoped Emitter;
// this indirection is necessary because the CEL program is compiled before
// the publish session exists.
//
// # Call forms
//
//	// Two-arg: publish value with no cursor.
//	<range>.emit(<iterVar>, <valueExpr>)                  -> map(string, dyn)
//
//	// Three-arg: publish value with per-element cursor.
//	<range>.emit(<iterVar>, <valueExpr>, <cursorExpr>)    -> map(string, dyn)
//
// The range must be traits.Lister or traits.Iterable. For each element,
// the macro evaluates the value expression, optionally the cursor expression,
// then calls Emitter.Emit. Iteration is sequential: cursor ordering is
// preserved.
//
// # Return value
//
// The macro returns {"published": <int>}. If a cursor expression was
// present and at least one event was published, the result also contains
// {"cursor": <lastCursor>}. If zero events were published, "cursor" is
// absent.
//
// # Error handling
//
// If iteration, expression evaluation, or Emit returns an error,
// iteration stops and the result includes {"error": <message>}. The
// "published" count and "cursor" reflect the state at the point of
// failure. Programs can check for the presence of the "error" key to
// distinguish incomplete processing from a clean completion.
func Emit(factory func() Emitter) cel.EnvOption {
	return cel.Lib(&emitLib{factory: factory})
}

type emitLib struct {
	factory func() Emitter

	mu sync.Mutex

	// bodies maps each @emit sentinel call's AST node ID to compile-time
	// metadata recorded during macro expansion.
	bodies map[int64]emitBody

	// bodyImpls maps @emit sentinel call ID → planned body Interpretable,
	// stored by the @emit_body decorator.
	bodyImpls map[int64]interpreter.Interpretable

	// cursorImpls maps @emit sentinel call ID → planned cursor Interpretable,
	// stored by the @emit_cursor decorator. Only populated for three-arg form.
	cursorImpls map[int64]interpreter.Interpretable
}

type emitBody struct {
	iterVar    string
	hasCursor  bool
	bodyCallID int64
	curCallID  int64
}

func (l *emitLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Variable(emitAccuVar, cel.ListType(cel.DynType)),

		// @emit(dyn, dyn) -> map(string, dyn)
		cel.Function(emitSentinel,
			cel.Overload(emitSentinel+"_impl",
				[]*cel.Type{cel.DynType, cel.DynType},
				mapStringDyn,
			),
		),
		// @emit_body(dyn) -> dyn
		cel.Function(emitBodySentinel,
			cel.Overload(emitBodySentinel+"_impl",
				[]*cel.Type{cel.DynType},
				cel.DynType,
			),
		),
		// @emit_cursor(dyn) -> dyn
		cel.Function(emitCursorSentinel,
			cel.Overload(emitCursorSentinel+"_impl",
				[]*cel.Type{cel.DynType},
				cel.DynType,
			),
		),

		cel.Macros(
			cel.ReceiverMacro("emit", 2, l.makeEmit2),
			cel.ReceiverMacro("emit", 3, l.makeEmit3),
		),
	}
}

func (l *emitLib) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{
		cel.CustomDecorator(l.emitBodyDecorator()),
		cel.CustomDecorator(l.emitCursorDecorator()),
		cel.CustomDecorator(l.emitDecorator()),
	}
}

// makeEmit2 handles range.emit(iterVar, valueExpr)
func (l *emitLib) makeEmit2(mef cel.MacroExprFactory, target ast.Expr, args []ast.Expr) (ast.Expr, *common.Error) {
	iterVar, err := emitRequireIdent(mef, args[0])
	if err != nil {
		return nil, err
	}
	bodyCall := mef.NewCall(emitBodySentinel, mef.Copy(args[1]))
	eb := emitBody{iterVar: iterVar, bodyCallID: bodyCall.ID()}
	return l.emitSentinelAST(mef, target, eb, func(rangeExpr ast.Expr) ast.Expr {
		accu := mef.NewIdent(emitAccuVar)
		return mef.NewComprehension(
			rangeExpr, iterVar, emitAccuVar,
			mef.NewList(),
			mef.NewLiteral(types.True),
			mef.NewCall(operators.Add, accu, mef.NewList(bodyCall)),
			mef.NewIdent(emitAccuVar),
		)
	})
}

// makeEmit3 handles range.emit(iterVar, valueExpr, cursorExpr)
func (l *emitLib) makeEmit3(mef cel.MacroExprFactory, target ast.Expr, args []ast.Expr) (ast.Expr, *common.Error) {
	iterVar, err := emitRequireIdent(mef, args[0])
	if err != nil {
		return nil, err
	}
	bodyCall := mef.NewCall(emitBodySentinel, mef.Copy(args[1]))
	cursorCall := mef.NewCall(emitCursorSentinel, mef.Copy(args[2]))
	eb := emitBody{
		iterVar:    iterVar,
		hasCursor:  true,
		bodyCallID: bodyCall.ID(),
		curCallID:  cursorCall.ID(),
	}
	return l.emitSentinelAST(mef, target, eb, func(rangeExpr ast.Expr) ast.Expr {
		accu := mef.NewIdent(emitAccuVar)
		return mef.NewComprehension(
			rangeExpr, iterVar, emitAccuVar,
			mef.NewList(),
			mef.NewLiteral(types.True),
			mef.NewCall(operators.Add, accu, mef.NewList(bodyCall, cursorCall)),
			mef.NewIdent(emitAccuVar),
		)
	})
}

func (l *emitLib) emitSentinelAST(mef cel.MacroExprFactory, target ast.Expr, eb emitBody, foldBuilder func(rangeExpr ast.Expr) ast.Expr) (ast.Expr, *common.Error) {
	fold := foldBuilder(mef.Copy(target))
	sentinel := mef.NewCall(emitSentinel, mef.Copy(target), fold)
	l.recordBody(sentinel.ID(), eb)
	return sentinel, nil
}

func (l *emitLib) recordBody(id int64, eb emitBody) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.bodies == nil {
		l.bodies = make(map[int64]emitBody)
	}
	l.bodies[id] = eb
}

func emitRequireIdent(mef cel.MacroExprFactory, e ast.Expr) (string, *common.Error) {
	if e.Kind() != ast.IdentKind {
		return "", mef.NewError(e.ID(), "emit: iteration variable must be a simple identifier")
	}
	name := e.AsIdent()
	if name == emitAccuVar || name == parser.AccumulatorName || name == parser.HiddenAccumulatorName {
		return "", mef.NewError(e.ID(), "emit: iteration variable conflicts with internal accumulator")
	}
	return name, nil
}

// emitBodyDecorator intercepts @emit_body calls, stores the body
// Interpretable, and returns the body to make the sentinel transparent.
func (l *emitLib) emitBodyDecorator() interpreter.InterpretableDecorator {
	return func(i interpreter.Interpretable) (interpreter.Interpretable, error) {
		call, ok := i.(interpreter.InterpretableCall)
		if !ok || call.Function() != emitBodySentinel {
			return i, nil
		}
		args := call.Args()
		if len(args) != 1 {
			return nil, fmt.Errorf("emit: @emit_body has %d args, expected 1", len(args))
		}
		l.mu.Lock()
		for emitID, eb := range l.bodies {
			if eb.bodyCallID == call.ID() {
				if l.bodyImpls == nil {
					l.bodyImpls = make(map[int64]interpreter.Interpretable)
				}
				l.bodyImpls[emitID] = args[0]
				break
			}
		}
		l.mu.Unlock()
		return args[0], nil
	}
}

// emitCursorDecorator intercepts @emit_cursor calls and stores the cursor
// Interpretable.
func (l *emitLib) emitCursorDecorator() interpreter.InterpretableDecorator {
	return func(i interpreter.Interpretable) (interpreter.Interpretable, error) {
		call, ok := i.(interpreter.InterpretableCall)
		if !ok || call.Function() != emitCursorSentinel {
			return i, nil
		}
		args := call.Args()
		if len(args) != 1 {
			return nil, fmt.Errorf("emit: @emit_cursor has %d args, expected 1", len(args))
		}
		l.mu.Lock()
		for emitID, eb := range l.bodies {
			if eb.curCallID == call.ID() {
				if l.cursorImpls == nil {
					l.cursorImpls = make(map[int64]interpreter.Interpretable)
				}
				l.cursorImpls[emitID] = args[0]
				break
			}
		}
		l.mu.Unlock()
		return args[0], nil
	}
}

// emitDecorator intercepts @emit calls and replaces them with emitFold
// Interpretables.
func (l *emitLib) emitDecorator() interpreter.InterpretableDecorator {
	return func(i interpreter.Interpretable) (interpreter.Interpretable, error) {
		call, ok := i.(interpreter.InterpretableCall)
		if !ok || call.Function() != emitSentinel {
			return i, nil
		}
		args := call.Args()
		if len(args) != 2 {
			return nil, fmt.Errorf("emit: sentinel call has %d args, expected 2", len(args))
		}
		iterRange := args[0]

		l.mu.Lock()
		eb, found := l.bodies[call.ID()]
		body := l.bodyImpls[call.ID()]
		cursor := l.cursorImpls[call.ID()]
		l.mu.Unlock()

		if !found {
			return nil, fmt.Errorf(
				"emit: @emit call %d has no registered metadata; "+
					"ensure the same Emit() lib instance is passed to both "+
					"cel.NewEnv and env.Program", call.ID())
		}
		if body == nil {
			return nil, fmt.Errorf(
				"emit: no body Interpretable for call %d; "+
					"ensure the same Emit() lib instance is passed to both "+
					"cel.NewEnv and env.Program", call.ID())
		}
		if eb.hasCursor && cursor == nil {
			return nil, fmt.Errorf(
				"emit: no cursor Interpretable for call %d; "+
					"ensure the same Emit() lib instance is passed to both "+
					"cel.NewEnv and env.Program", call.ID())
		}

		return &emitFold{
			id:        call.ID(),
			iterVar:   eb.iterVar,
			hasCursor: eb.hasCursor,
			iterRange: iterRange,
			body:      body,
			cursor:    cursor,
			factory:   l.factory,
		}, nil
	}
}

// emitFold is the Interpretable that replaces each @emit sentinel at runtime.
// It iterates sequentially, evaluating the body and optional cursor for each
// element, and calls Emitter.Emit for each.
type emitFold struct {
	id        int64
	iterVar   string
	hasCursor bool
	iterRange interpreter.Interpretable
	body      interpreter.Interpretable
	cursor    interpreter.Interpretable // nil when !hasCursor
	factory   func() Emitter
}

var _ interpreter.Interpretable = (*emitFold)(nil)

func (e *emitFold) ID() int64 { return e.id }

func (e *emitFold) Eval(activation interpreter.Activation) ref.Val {
	rangeVal := e.iterRange.Eval(activation)
	if types.IsError(rangeVal) {
		return rangeVal
	}

	emitter := e.factory()
	if emitter == nil {
		return types.NewErr("emit: no emitter available")
	}

	var iter traits.Iterator
	switch rv := rangeVal.(type) {
	case traits.Lister:
		iter = rv.Iterator()
	case traits.Iterable:
		iter = rv.Iterator()
	default:
		return types.NewErr("emit: receiver must be a list or iterable, got %T", rangeVal)
	}

	var (
		count      int64
		lastCursor any
		errMsg     string
	)
	for iter.HasNext() == types.True {
		elem := iter.Next()
		if types.IsError(elem) {
			errMsg = elem.(*types.Err).Error()
			break
		}

		bindings := map[string]any{e.iterVar: elem}
		child, err := interpreter.NewActivation(bindings)
		if err != nil {
			return types.WrapErr(err)
		}
		hier := interpreter.NewHierarchicalActivation(activation, child)

		bodyVal := e.body.Eval(hier)
		if types.IsError(bodyVal) {
			errMsg = bodyVal.(*types.Err).Error()
			break
		}

		var cursorVal any
		if e.hasCursor {
			cv := e.cursor.Eval(hier)
			if types.IsError(cv) {
				errMsg = cv.(*types.Err).Error()
				break
			}
			cursorVal = nativeValue(cv)
		}

		eventVal := nativeValue(bodyVal)
		if err := emitter.Emit(eventVal, cursorVal); err != nil {
			errMsg = err.Error()
			break
		}
		count++
		if e.hasCursor {
			lastCursor = cursorVal
		}
	}

	result := map[string]any{"published": count}
	if e.hasCursor && count > 0 {
		result["cursor"] = lastCursor
	}
	if errMsg != "" {
		result["error"] = errMsg
	}
	return types.DefaultTypeAdapter.NativeToValue(result)
}

// nativeValue converts a ref.Val to a Go-native value suitable for passing
// to the Emitter. Maps and lists are converted to their native representations;
// scalars are unwrapped via Value().
func nativeValue(v ref.Val) any {
	switch v := v.(type) {
	case traits.Mapper:
		m, err := v.ConvertToNative(reflectMapStringAnyType)
		if err == nil {
			return m
		}
	case traits.Lister:
		l, err := v.ConvertToNative(reflectAnySliceType)
		if err == nil {
			return l
		}
	}
	return v.Value()
}
