// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package lib

import (
	"fmt"
	"runtime"
	"slices"
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

// Parallel returns a cel.EnvOption that configures the parallel macro for
// concurrent application of an expression to every element of a list or map.
//
// # Call forms
//
// parallel mirrors the call forms of the built-in map macro:
//
//	// (1) One-variable — apply expr to every element.
//	<list>.parallel(<iterVar>, <expr>) -> list<dyn>
//	<map>.parallel(<iterVar>, <expr>)  -> list<dyn>
//
//	// (2a) Predicate filter — apply expr only where pred is true.
//	<list>.parallel(<iterVar>, <pred>, <expr>) -> list<dyn>
//	<map>.parallel(<iterVar>, <pred>, <expr>)  -> list<dyn>
//
//	// (2b) Two-variable — bind index/key and value simultaneously.
//	<list>.parallel(<iterVar>, <iterVar2>, <expr>) -> list<dyn>
//	<map>.parallel(<iterVar>, <iterVar2>, <expr>)  -> list<dyn>
//
// Forms (2a) and (2b) are distinguished by their second argument: if it is a
// plain identifier it is treated as iterVar2 (two-variable form); otherwise it
// is treated as a boolean predicate (filter form). This is the same rule used
// by map.
//
// For list ranges in the two-variable form, iterVar is the zero-based element
// index and iterVar2 is the element value. For map ranges, iterVar is the key
// and iterVar2 is the corresponding value.
//
// # Concurrency
//
// perCall controls the maximum number of goroutines used simultaneously by a
// single .parallel() call. If perCall ≤ 0 it defaults to runtime.GOMAXPROCS(0).
// When perCall == 1 the macro degenerates to the sequential map macro: no
// goroutines are spawned and there is no overhead.
//
// globalCap bounds the total number of concurrent leaf body evaluations across
// all .parallel() calls in the program. If globalCap ≤ 0, no global limit is
// applied. globalCap < perCall is valid: outer goroutines driving nested
// .parallel() calls do useful dispatch work, not just leaf evaluations.
//
// # Concurrency safety
//
// Body and predicate expressions must be safe for concurrent evaluation.
// Custom functions called from inside a parallel body must not rely on shared
// mutable state unless they provide their own synchronisation.
//
// # Error handling
//
// If any element's expression returns an error, the first error in iteration
// order is returned and no result list is produced, consistent with how CEL's
// built-in map macro propagates errors. When one element errors, remaining
// evaluations still run to completion — there is no early termination.
//
// # Notes
//
// A single Parallel lib instance must not be shared across concurrent
// cel.Env constructions. One instance per pipeline configuration is the
// expected pattern, consistent with all other mito libs.
//
// # Examples
//
//	// Fetch every URL concurrently.
//	urls.parallel(u, get(u))
//
//	// Fetch only HTTPS URLs concurrently.
//	urls.parallel(u, u.startsWith("https://"), get(u))
//
//	// Two-variable list form: enrich each item with its position.
//	items.parallel(i, item, item.with({"seq": i}))
//
//	// Two-variable map form: key and value both directly in scope.
//	endpoints.parallel(svc, url, get(url).Body.decode_json().with({"svc":svc}))
func Parallel(perCall, globalCap int) cel.EnvOption {
	var globalSem chan struct{}
	if globalCap > 0 {
		globalSem = make(chan struct{}, globalCap)
	}
	return cel.Lib(&parallelLib{perCall: perCall, globalSem: globalSem})
}

// parallelLib implements cel.Library.
//
// # Design: three sentinel functions
//
// The cel-go interpreter plans a comprehension as an unexported *evalFold
// whose sub-expressions (iterRange, step, etc.) are not accessible via any
// public interface. Rather than using an oracle strategy (which requires
// synthetic collections and range-variable shadowing with a known limitation
// for complex range expressions), we emit additional sentinel function calls
// inside the AST so that the planned sub-Interpretables are recoverable via
// InterpretableCall.Args():
//
//	@parallel(rangeExpr, fold)
//	  fold step contains: @parallel_body(transformExpr)
//	  fold step also contains (predicate form only): @parallel_pred(predExpr)
//
// Three decorators intercept these sentinel calls by function name:
//   - @parallel_body: stores Args()[0] (transform) in bodyImpls
//   - @parallel_pred: stores Args()[0] (predicate) in predImpls
//   - @parallel:      retrieves iterRange from Args()[0], body from bodyImpls,
//     pred from predImpls, and builds a parallelFold
//
// parallelFold.Eval calls body.Eval(elementActivation) directly — no oracle,
// no synthetic collection, no range-variable shadowing.
//
// # Why we never call parser.MakeMap inside emitSentinel
//
// parser.MakeMap uses parser.AccumulatorName ("@result") as the accumulator
// variable in the comprehension it emits. When that comprehension is embedded
// as an argument to the @parallel sentinel call, the type-checker sees "@result"
// as a free variable reference and rejects it. We therefore always emit our
// own comprehensions using parallelAccuVar ("@parallel_result"), which we
// declare explicitly as a cel.Variable in CompileOptions.
type parallelLib struct {
	perCall   int
	globalSem chan struct{} // nil when no global limit

	mu sync.Mutex

	// bodies maps each @parallel sentinel call's AST node ID to the per-call
	// metadata recorded at macro expansion time.
	bodies map[int64]parallelBody

	// bodyImpls maps each @parallel sentinel call's AST node ID to the planned
	// transform Interpretable, stored by the @parallel_body decorator.
	bodyImpls map[int64]interpreter.Interpretable

	// predImpls maps each @parallel sentinel call's AST node ID to the planned
	// predicate Interpretable, stored by the @parallel_pred decorator.
	// Only populated for the predicate filter form.
	predImpls map[int64]interpreter.Interpretable
}

// parallelBody carries compile-time metadata for one parallel call site.
type parallelBody struct {
	iterVar        string // first iteration variable
	iterVar2       string // second iteration variable; non-empty for two-variable form
	hasPred        bool   // true for the (iterVar, pred, expr) form
	nestedParallel bool   // true when the body expression contains a nested @parallel
	bodyCallID     int64  // node ID of the @parallel_body sentinel call
	predCallID     int64  // node ID of the @parallel_pred sentinel call; zero if !hasPred
}

// CompileOptions registers the three sentinel function declarations, the
// parallelAccuVar variable declaration, and both macro overloads.
//
// The parallelAccuVar declaration is required because the inner comprehensions
// reference it as an accumulator, and the type-checker treats accumulator
// references as variable lookups when the comprehension is nested inside a
// function call argument.
func (l *parallelLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		// Declare parallelAccuVar so the type-checker accepts its reference
		// inside the comprehension step and result expressions.
		cel.Variable(parallelAccuVar, cel.ListType(cel.DynType)),
		// @parallel(dyn, dyn) -> list(dyn)
		cel.Function(parallelSentinel,
			cel.Overload(parallelSentinel+"_impl",
				[]*cel.Type{cel.DynType, cel.DynType},
				cel.ListType(cel.DynType),
			),
		),
		// @parallel_body(dyn) -> dyn
		cel.Function(parallelBodySentinel,
			cel.Overload(parallelBodySentinel+"_impl",
				[]*cel.Type{cel.DynType},
				cel.DynType,
			),
		),
		// @parallel_pred(dyn) -> dyn
		cel.Function(parallelPredSentinel,
			cel.Overload(parallelPredSentinel+"_impl",
				[]*cel.Type{cel.DynType},
				cel.DynType,
			),
		),
		cel.Macros(
			// (1)    range.parallel(v, expr)
			cel.ReceiverMacro("parallel", 2, l.makeParallel2),
			// (2a/b) range.parallel(v, pred_or_v2, expr)
			cel.ReceiverMacro("parallel", 3, l.makeParallel3),
		),
	}
}

// ProgramOptions installs the three decorators. The body and predicate
// decorators must run before the parallel decorator so that by the time
// parallelDecorator fires, all Interpretables are already stored.
func (l *parallelLib) ProgramOptions() []cel.ProgramOption {
	perCall := l.perCall
	if perCall <= 0 {
		perCall = runtime.GOMAXPROCS(0)
	}
	return []cel.ProgramOption{
		cel.CustomDecorator(l.parallelBodyDecorator()),
		cel.CustomDecorator(l.parallelPredDecorator()),
		cel.CustomDecorator(l.parallelDecorator(perCall)),
	}
}

// makeParallel2 handles: range.parallel(iterVar, expr)
//
// When perCall == 1 it degenerates to map(iterVar, expr) by emitting a plain
// comprehension that the standard interpreter executes sequentially.
func (l *parallelLib) makeParallel2(mef cel.MacroExprFactory, target ast.Expr, args []ast.Expr) (ast.Expr, *common.Error) {
	iterVar, err := requireIdent(mef, args[0])
	if err != nil {
		return nil, err
	}
	if l.perCall < 2 {
		return l.makeSeqComprehension(mef, target, iterVar, nil, args[1]), nil
	}
	bodyCall := mef.NewCall(parallelBodySentinel, args[1])
	pb := parallelBody{
		iterVar:        iterVar,
		nestedParallel: containsParallel(args[1]),
		bodyCallID:     bodyCall.ID(),
	}
	return l.emitSentinel(mef, target, pb, func(rangeExpr ast.Expr) ast.Expr {
		accu := mef.NewIdent(parallelAccuVar)
		return mef.NewComprehension(
			rangeExpr, iterVar, parallelAccuVar,
			mef.NewList(),
			mef.NewLiteral(types.True),
			mef.NewCall(operators.Add, accu, mef.NewList(bodyCall)),
			mef.NewIdent(parallelAccuVar),
		)
	})
}

// makeParallel3 handles:
//
//	(2a) range.parallel(iterVar, pred, expr)     - predicate filter form
//	(2b) range.parallel(iterVar, iterVar2, expr) - two-variable form
//
// If args[1] is a plain identifier it is iterVar2 (2b); otherwise it is a
// boolean predicate (2a). This matches map's own disambiguation rule.
func (l *parallelLib) makeParallel3(mef cel.MacroExprFactory, target ast.Expr, args []ast.Expr) (ast.Expr, *common.Error) {
	iterVar, err := requireIdent(mef, args[0])
	if err != nil {
		return nil, err
	}

	if args[1].Kind() == ast.IdentKind {
		// Two-variable form (2b).
		iterVar2, err := requireIdent(mef, args[1])
		if err != nil {
			return nil, err
		}
		if iterVar == iterVar2 {
			return nil, mef.NewError(args[1].ID(), "parallel: iteration variables must be distinct")
		}
		if l.perCall < 2 {
			return l.makeSeqComprehensionTwoVar(mef, target, iterVar, iterVar2, args[2]), nil
		}
		bodyCall := mef.NewCall(parallelBodySentinel, args[2])
		pb := parallelBody{
			iterVar:        iterVar,
			iterVar2:       iterVar2,
			nestedParallel: containsParallel(args[2]),
			bodyCallID:     bodyCall.ID(),
		}
		return l.emitSentinel(mef, target, pb, func(rangeExpr ast.Expr) ast.Expr {
			accu := mef.NewIdent(parallelAccuVar)
			return mef.NewComprehensionTwoVar(
				rangeExpr, iterVar, iterVar2, parallelAccuVar,
				mef.NewList(),
				mef.NewLiteral(types.True),
				mef.NewCall(operators.Add, accu, mef.NewList(bodyCall)),
				mef.NewIdent(parallelAccuVar),
			)
		})
	}

	// Predicate filter form (2a).
	if l.perCall < 2 {
		return l.makeSeqComprehension(mef, target, iterVar, args[1], args[2]), nil
	}
	predCall := mef.NewCall(parallelPredSentinel, args[1])
	bodyCall := mef.NewCall(parallelBodySentinel, args[2])
	pb := parallelBody{
		iterVar:        iterVar,
		hasPred:        true,
		nestedParallel: containsParallel(args[2]),
		predCallID:     predCall.ID(),
		bodyCallID:     bodyCall.ID(),
	}
	return l.emitSentinel(mef, target, pb, func(rangeExpr ast.Expr) ast.Expr {
		accu := mef.NewIdent(parallelAccuVar)
		// step: accu + (@parallel_pred(pred) ? [@parallel_body(expr)] : [])
		return mef.NewComprehension(
			rangeExpr, iterVar, parallelAccuVar,
			mef.NewList(),
			mef.NewLiteral(types.True),
			mef.NewCall(operators.Add,
				accu,
				mef.NewCall(operators.Conditional,
					predCall,
					mef.NewList(bodyCall),
					mef.NewList(),
				),
			),
			mef.NewIdent(parallelAccuVar),
		)
	})
}

// makeSeqComprehension emits a sequential one-variable comprehension using
// parallelAccuVar as the accumulator, used for the perCall==1 path.
// If pred is non-nil it is incorporated as a conditional step (filter form).
func (l *parallelLib) makeSeqComprehension(mef cel.MacroExprFactory, target ast.Expr, iterVar string, pred, body ast.Expr) ast.Expr {
	accu := mef.NewIdent(parallelAccuVar)
	bodyList := mef.NewList(mef.Copy(body))
	var step ast.Expr
	if pred != nil {
		step = mef.NewCall(operators.Add,
			accu,
			mef.NewCall(operators.Conditional, mef.Copy(pred), bodyList, mef.NewList()),
		)
	} else {
		step = mef.NewCall(operators.Add, accu, bodyList)
	}
	return mef.NewComprehension(
		target, iterVar, parallelAccuVar,
		mef.NewList(),
		mef.NewLiteral(types.True),
		step,
		mef.NewIdent(parallelAccuVar),
	)
}

// makeSeqComprehensionTwoVar emits a sequential two-variable comprehension
// using parallelAccuVar as the accumulator, used for the perCall==1 path.
func (l *parallelLib) makeSeqComprehensionTwoVar(mef cel.MacroExprFactory, target ast.Expr, iterVar, iterVar2 string, body ast.Expr) ast.Expr {
	accu := mef.NewIdent(parallelAccuVar)
	return mef.NewComprehensionTwoVar(
		target, iterVar, iterVar2, parallelAccuVar,
		mef.NewList(),
		mef.NewLiteral(types.True),
		mef.NewCall(operators.Add, accu, mef.NewList(mef.Copy(body))),
		mef.NewIdent(parallelAccuVar),
	)
}

// emitSentinel builds and records the @parallel(rangeExpr, fold) AST node.
// foldBuilder receives a copy of target as its range expression and must
// embed the bodyCall (and predCall for the filter form) in the fold step.
func (l *parallelLib) emitSentinel(mef cel.MacroExprFactory, target ast.Expr, pb parallelBody, foldBuilder func(rangeExpr ast.Expr) ast.Expr) (ast.Expr, *common.Error) {
	fold := foldBuilder(mef.Copy(target))
	sentinel := mef.NewCall(parallelSentinel, mef.Copy(target), fold)
	l.recordBody(sentinel.ID(), pb)
	return sentinel, nil
}

// parallelSentinel is the name of the no-op sentinel function the parallel
// macros emit as the outermost wrapper. Its two planned arguments are
// accessible to the decorator via the public InterpretableCall.Args() API:
//
//	args[0] → planned range Interpretable  (evaluates to the list or map)
//	args[1] → planned fold Interpretable   (discarded at runtime; present
//	           only so the type-checker sees a well-formed comprehension)
//
// The sentinel is never called at runtime: the decorator replaces every
// InterpretableCall whose Function() is parallelSentinel with a parallelFold.
const parallelSentinel = "@parallel"

// requireIdent asserts that e is a plain identifier and returns its name.
func requireIdent(mef cel.MacroExprFactory, e ast.Expr) (string, *common.Error) {
	if e.Kind() != ast.IdentKind {
		return "", mef.NewError(e.ID(), "parallel: iteration variable must be a simple identifier")
	}
	name := e.AsIdent()
	if name == parallelAccuVar || name == parser.AccumulatorName || name == parser.HiddenAccumulatorName {
		return "", mef.NewError(e.ID(), "parallel: iteration variable conflicts with internal accumulator")
	}
	return name, nil
}

// containsParallel reports whether expr contains a nested @parallel sentinel
// call anywhere in its subtree. Macros expand bottom-up, so when the outer
// parallel macro fires the inner is already rewritten to @parallel(...).
func containsParallel(expr ast.Expr) bool {
	switch expr.Kind() {
	case ast.CallKind:
		c := expr.AsCall()
		if c.FunctionName() == parallelSentinel {
			return true
		}
		if c.IsMemberFunction() && containsParallel(c.Target()) {
			return true
		}
		return slices.ContainsFunc(c.Args(), containsParallel)
	case ast.ComprehensionKind:
		c := expr.AsComprehension()
		return containsParallel(c.IterRange()) ||
			containsParallel(c.AccuInit()) ||
			containsParallel(c.LoopCondition()) ||
			containsParallel(c.LoopStep()) ||
			containsParallel(c.Result())
	case ast.ListKind:
		return slices.ContainsFunc(expr.AsList().Elements(), containsParallel)
	case ast.MapKind:
		m := expr.AsMap()
		for _, entry := range m.Entries() {
			e := entry.AsMapEntry()
			if containsParallel(e.Key()) || containsParallel(e.Value()) {
				return true
			}
		}
	case ast.SelectKind:
		return containsParallel(expr.AsSelect().Operand())
	case ast.StructKind:
		for _, f := range expr.AsStruct().Fields() {
			if containsParallel(f.AsStructField().Value()) {
				return true
			}
		}
	}
	return false
}

func (l *parallelLib) recordBody(id int64, pb parallelBody) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.bodies == nil {
		l.bodies = make(map[int64]parallelBody)
	}
	l.bodies[id] = pb
}

// parallelAccuVar is the accumulator variable name used in the inner folds
// emitted by the parallel macros. The leading '@' places it outside the space
// of valid user-written identifiers, preventing collisions.
//
// It must also be declared as a cel.Variable in CompileOptions so that the
// type-checker accepts the accumulator reference inside the comprehension body.
const parallelAccuVar = "@parallel_result"

// parallelBodyDecorator intercepts planned @parallel_body calls, stores
// Args()[0] as the transform Interpretable keyed on the parent @parallel
// call's ID, and returns Args()[0] to make the sentinel transparent.
func (l *parallelLib) parallelBodyDecorator() interpreter.InterpretableDecorator {
	return func(i interpreter.Interpretable) (interpreter.Interpretable, error) {
		call, ok := i.(interpreter.InterpretableCall)
		if !ok || call.Function() != parallelBodySentinel {
			return i, nil
		}
		args := call.Args()
		if len(args) != 1 {
			return nil, fmt.Errorf("parallel: @parallel_body has %d args, expected 1", len(args))
		}
		l.mu.Lock()
		for parallelID, pb := range l.bodies {
			if pb.bodyCallID == call.ID() {
				if l.bodyImpls == nil {
					l.bodyImpls = make(map[int64]interpreter.Interpretable)
				}
				l.bodyImpls[parallelID] = args[0]
				break
			}
		}
		l.mu.Unlock()
		return args[0], nil
	}
}

// parallelBodySentinel is the name of a second no-op sentinel that wraps the
// per-element transform expression inside the fold step emitted by the macro.
// Because it is planned as an InterpretableCall, its sole argument — the
// planned transform Interpretable — is recoverable via Args()[0] using the
// fully public API. A dedicated decorator intercepts these calls and stores
// the transform Interpretable in parallelLib.bodyImpls.
//
// This eliminates the oracle strategy entirely: parallelFold.Eval calls
// body.Eval(elementActivation) directly, with no synthetic collections and
// no range-variable shadowing. Complex range expressions such as
// state.items.parallel(...) work correctly.
//
// TODO: if cel-go ever exports an InterpretableFold interface that exposes
// the step Interpretable of a planned comprehension, both sentinel functions
// become unnecessary and the whole implementation simplifies to a type
// assertion in the decorator. Worth filing upstream.
const parallelBodySentinel = "@parallel_body"

// parallelPredDecorator intercepts planned @parallel_pred calls, stores
// Args()[0] as the predicate Interpretable keyed on the parent @parallel
// call's ID, and returns Args()[0] to make the sentinel transparent.
func (l *parallelLib) parallelPredDecorator() interpreter.InterpretableDecorator {
	return func(i interpreter.Interpretable) (interpreter.Interpretable, error) {
		call, ok := i.(interpreter.InterpretableCall)
		if !ok || call.Function() != parallelPredSentinel {
			return i, nil
		}
		args := call.Args()
		if len(args) != 1 {
			return nil, fmt.Errorf("parallel: @parallel_pred has %d args, expected 1", len(args))
		}
		l.mu.Lock()
		for parallelID, pb := range l.bodies {
			if pb.predCallID == call.ID() {
				if l.predImpls == nil {
					l.predImpls = make(map[int64]interpreter.Interpretable)
				}
				l.predImpls[parallelID] = args[0]
				break
			}
		}
		l.mu.Unlock()
		return args[0], nil
	}
}

// parallelPredSentinel is the name of a third no-op sentinel that wraps the
// predicate expression in the filter form range.parallel(v, pred, expr).
// It is treated identically to parallelBodySentinel: a dedicated decorator
// intercepts it and stores the planned predicate Interpretable in
// parallelLib.predImpls so that parallelFold.Eval can gate each body
// evaluation on the predicate without needing the fold's conditional step.
const parallelPredSentinel = "@parallel_pred"

// parallelDecorator intercepts planned @parallel calls and replaces them with
// parallelFold Interpretables. Detection is by function name, which is robust
// against node ID renumbering by constant-folding optimisations.
func (l *parallelLib) parallelDecorator(perCall int) interpreter.InterpretableDecorator {
	return func(i interpreter.Interpretable) (interpreter.Interpretable, error) {
		call, ok := i.(interpreter.InterpretableCall)
		if !ok || call.Function() != parallelSentinel {
			return i, nil
		}
		args := call.Args()
		if len(args) != 2 {
			return nil, fmt.Errorf("parallel: sentinel call has %d args, expected 2", len(args))
		}
		iterRange := args[0] // args[1] is the fold; discarded here

		l.mu.Lock()
		pb, found := l.bodies[call.ID()]
		body := l.bodyImpls[call.ID()]
		pred := l.predImpls[call.ID()] // nil if !hasPred
		l.mu.Unlock()

		// A @parallel call with no registered metadata means this decorator is
		// running against an AST compiled by a different parallelLib instance.
		// This is always a programming error: return a clear message rather than
		// silently leaving an unevaluable sentinel call in the program.
		if !found {
			return nil, fmt.Errorf(
				"parallel: @parallel call %d has no registered metadata; "+
					"ensure the same Parallel() lib instance is passed to both "+
					"cel.NewEnv and env.Program", call.ID())
		}
		if body == nil {
			return nil, fmt.Errorf(
				"parallel: no body Interpretable for call %d; "+
					"ensure the same Parallel() lib instance is passed to both "+
					"cel.NewEnv and env.Program", call.ID())
		}
		if pb.hasPred && pred == nil {
			return nil, fmt.Errorf(
				"parallel: no predicate Interpretable for call %d; "+
					"ensure the same Parallel() lib instance is passed to both "+
					"cel.NewEnv and env.Program", call.ID())
		}

		return &parallelFold{
			id:        call.ID(),
			iterVar:   pb.iterVar,
			iterVar2:  pb.iterVar2,
			hasPred:   pb.hasPred,
			leafBody:  !pb.nestedParallel,
			iterRange: iterRange,
			body:      body,
			pred:      pred,
			perCall:   perCall,
			globalSem: l.globalSem,
		}, nil
	}
}

// parallelFold is the concurrent Interpretable that replaces each @parallel
// sentinel call at runtime.
//
// For each element the planned body Interpretable is evaluated directly:
//
//	body.Eval(interpreter.NewHierarchicalActivation(outer, elementActivation))
//
// where elementActivation binds iterVar (and iterVar2 for the two-variable
// form) to the current element's value(s). The outer activation provides all
// other names, including the range expression itself, so complex receivers
// such as state.items.parallel(...) work correctly.
//
// For the predicate form, pred.Eval(elementActivation) is called first; the
// body is skipped if the predicate is false, and the element is omitted from
// the result list.
//
// Elements are dispatched through a semaphore of size perCall. Results are
// written into a pre-allocated slice at the element's original index so that
// output order matches input order regardless of goroutine completion order.
type parallelFold struct {
	id        int64
	iterVar   string
	iterVar2  string
	hasPred   bool
	leafBody  bool // true when body is not itself a nested parallelFold
	iterRange interpreter.Interpretable
	body      interpreter.Interpretable
	pred      interpreter.Interpretable // nil when !hasPred
	perCall   int
	globalSem chan struct{} // nil when no global limit
}

// ID implements interpreter.Interpretable.
func (p *parallelFold) ID() int64 { return p.id }

// Eval implements interpreter.Interpretable.
func (p *parallelFold) Eval(activation interpreter.Activation) ref.Val {
	// Evaluate the range expression once.
	rangeVal := p.iterRange.Eval(activation)
	if types.IsError(rangeVal) {
		return rangeVal
	}

	// Collect per-goroutine element descriptors.
	type elem struct {
		primary   ref.Val // iterVar binding
		secondary ref.Val // iterVar2 binding; zero value for one-variable forms
	}

	var elems []elem
	twoVar := p.iterVar2 != ""

	switch rv := rangeVal.(type) {
	case traits.Lister:
		it := rv.Iterator()
		if twoVar {
			for idx := types.Int(0); it.HasNext() == types.True; idx++ {
				elems = append(elems, elem{primary: idx, secondary: it.Next()})
			}
		} else {
			for it.HasNext() == types.True {
				elems = append(elems, elem{primary: it.Next()})
			}
		}
	case traits.Mapper:
		it := rv.Iterator()
		if twoVar {
			for it.HasNext() == types.True {
				k := it.Next()
				elems = append(elems, elem{primary: k, secondary: rv.Get(k)})
			}
		} else {
			for it.HasNext() == types.True {
				elems = append(elems, elem{primary: it.Next()})
			}
		}
	default:
		return types.NewErr("parallel: receiver must be a list or map, got %T", rangeVal)
	}

	n := len(elems)
	if n == 0 {
		return types.DefaultTypeAdapter.NativeToValue([]ref.Val{})
	}

	var filtered []bool
	if p.hasPred {
		filtered = make([]bool, n)
	}
	results := make([]ref.Val, n)

	sem := make(chan struct{}, p.perCall)
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var seenErr ref.Val

	// Concurrent body.Eval and pred.Eval calls below assume that
	// Interpretable.Eval is safe for concurrent use when each call receives
	// an independent Activation. This holds for cel-go v0.28 — the interpreter
	// structures (evalConst, evalAttr, evalOr, evalFold, etc.) carry no mutable
	// per-evaluation state. This is not a documented guarantee.
	// TestParallel_ConcurrentEvalRace is a race-detector trip wire that will
	// catch regressions if a future cel-go release introduces shared mutable
	// state into any Interpretable implementation.
	for i, e := range elems {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, e elem) {
			defer wg.Done()
			defer func() { <-sem }()

			if p.leafBody && p.globalSem != nil {
				p.globalSem <- struct{}{}
				defer func() { <-p.globalSem }()
			}

			bindings := map[string]any{p.iterVar: e.primary}
			if twoVar {
				bindings[p.iterVar2] = e.secondary
			}
			childActivation, err := interpreter.NewActivation(bindings)
			if err != nil {
				v := types.WrapErr(err)
				results[idx] = v
				errMu.Lock()
				if seenErr == nil {
					seenErr = v
				}
				errMu.Unlock()
				return
			}
			hier := interpreter.NewHierarchicalActivation(activation, childActivation)

			// Predicate check.
			if p.hasPred {
				predVal := p.pred.Eval(hier)
				if types.IsError(predVal) {
					results[idx] = predVal
					errMu.Lock()
					if seenErr == nil {
						seenErr = predVal
					}
					errMu.Unlock()
					return
				}
				if predVal != types.True {
					filtered[idx] = true
					return
				}
			}

			result := p.body.Eval(hier)
			results[idx] = result
			if types.IsError(result) {
				errMu.Lock()
				if seenErr == nil {
					seenErr = result
				}
				errMu.Unlock()
			}
		}(i, e)
	}

	wg.Wait()

	// Surface the first error in iteration order (deterministic across runs).
	if seenErr != nil {
		for _, r := range results {
			if types.IsError(r) {
				return r
			}
		}
		return seenErr // unreachable in practice
	}

	// Assemble the result list, omitting filtered elements.
	out := make([]ref.Val, 0, n)
	for i, r := range results {
		if filtered != nil && filtered[i] {
			continue
		}
		out = append(out, r)
	}
	return types.DefaultTypeAdapter.NativeToValue(out)
}
