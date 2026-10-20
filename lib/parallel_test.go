// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package lib

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/google/cel-go/interpreter"
)

// form (1): range.parallel(v, expr)

func TestParallel_List_Basic(t *testing.T) {
	env := parallelTestEnv(t, 0)
	prg := parallelProgram(t, env, `items.parallel(x, x * 2)`)
	out := parallelEval(t, prg, map[string]any{"items": []any{1, 2, 3, 4, 5}})
	assertOrderedList(t, out, []ref.Val{
		types.Int(2), types.Int(4), types.Int(6), types.Int(8), types.Int(10),
	})
}

func TestParallel_List_Empty(t *testing.T) {
	env := parallelTestEnv(t, 0)
	prg := parallelProgram(t, env, `items.parallel(x, x)`)
	out := parallelEval(t, prg, map[string]any{"items": []any{}})
	assertOrderedList(t, out, nil)
}

func TestParallel_List_OuterScopeVisible(t *testing.T) {
	env := parallelTestEnv(t, 0)
	prg := parallelProgram(t, env, `items.parallel(x, x + suffix)`)
	out := parallelEval(t, prg, map[string]any{
		"items":  []any{"hello", "world"},
		"suffix": "!",
	})
	assertOrderedList(t, out, []ref.Val{types.String("hello!"), types.String("world!")})
}

func TestParallel_List_PreservesOrder(t *testing.T) {
	env := parallelTestEnv(t, 8)
	prg := parallelProgram(t, env, `items.parallel(x, x)`)
	input := make([]any, 200)
	for i := range input {
		input[i] = i
	}
	for run := 0; run < 20; run++ {
		out := parallelEval(t, prg, map[string]any{"items": input})
		list := out.(traits.Lister)
		sz := int(list.Size().(types.Int))
		for i := 0; i < sz; i++ {
			got := list.Get(types.Int(i))
			if got.(types.Int) != types.Int(i) {
				t.Fatalf("run %d index %d: got %v want %d", run, i, got, i)
			}
		}
	}
}

func TestParallel_List_ActivationIsolation(t *testing.T) {
	// Each goroutine must see its own iterVar, not a shared value.
	env := parallelTestEnv(t, 8)
	prg := parallelProgram(t, env, `items.parallel(x, x * x)`)
	input := make([]any, 50)
	for i := range input {
		input[i] = i
	}
	out := parallelEval(t, prg, map[string]any{"items": input})
	want := make([]ref.Val, 50)
	for i := range want {
		want[i] = types.Int(i * i)
	}
	assertOrderedList(t, out, want)
}

func TestParallel_Map_OneVar(t *testing.T) {
	// Over a map range, iterVar is bound to each key.
	// The outer activation keeps the map itself in scope.
	env := parallelTestEnv(t, 0)
	prg := parallelProgram(t, env, `m.parallel(k, m[k] * 10)`)
	out := parallelEval(t, prg, map[string]any{
		"m": map[string]any{"a": 1, "b": 2, "c": 3},
	})
	assertMultiset(t, out, []ref.Val{types.Int(10), types.Int(20), types.Int(30)})
}

// form (2a): range.parallel(v, pred, expr)

func TestParallel_Pred_Basic(t *testing.T) {
	env := parallelTestEnv(t, 0)
	prg := parallelProgram(t, env, `items.parallel(x, x % 2 == 0, x * 2)`)
	out := parallelEval(t, prg, map[string]any{"items": []any{1, 2, 3, 4}})
	assertOrderedList(t, out, []ref.Val{types.Int(4), types.Int(8)})
}

func TestParallel_Pred_NonePass(t *testing.T) {
	env := parallelTestEnv(t, 0)
	prg := parallelProgram(t, env, `items.parallel(x, x > 100, x)`)
	out := parallelEval(t, prg, map[string]any{"items": []any{1, 2, 3}})
	assertOrderedList(t, out, nil)
}

func TestParallel_Pred_AllPass(t *testing.T) {
	env := parallelTestEnv(t, 0)
	prg := parallelProgram(t, env, `items.parallel(x, x > 0, x * 3)`)
	out := parallelEval(t, prg, map[string]any{"items": []any{1, 2, 3}})
	assertOrderedList(t, out, []ref.Val{types.Int(3), types.Int(6), types.Int(9)})
}

func TestParallel_Pred_PreservesOrder(t *testing.T) {
	// Output must be in input order among the elements that pass the predicate.
	env := parallelTestEnv(t, 8)
	prg := parallelProgram(t, env, `items.parallel(x, x % 2 == 0, x)`)
	input := make([]any, 100)
	for i := range input {
		input[i] = i
	}
	for run := 0; run < 10; run++ {
		out := parallelEval(t, prg, map[string]any{"items": input})
		list := out.(traits.Lister)
		sz := int(list.Size().(types.Int))
		if sz != 50 {
			t.Fatalf("run %d: expected 50 results, got %d", run, sz)
		}
		for i := 0; i < sz; i++ {
			got := list.Get(types.Int(i))
			if got.(types.Int) != types.Int(i*2) {
				t.Fatalf("run %d index %d: got %v want %d", run, i, got, i*2)
			}
		}
	}
}

func TestParallel_Pred_Map(t *testing.T) {
	env := parallelTestEnv(t, 0)
	prg := parallelProgram(t, env, `m.parallel(k, k.startsWith("h"), m[k] * 10)`)
	out := parallelEval(t, prg, map[string]any{
		"m": map[string]any{"hello": 1, "world": 2, "hi": 3},
	})
	assertMultiset(t, out, []ref.Val{types.Int(10), types.Int(30)})
}

func TestParallel_Pred_OuterScopeVisible(t *testing.T) {
	env := parallelTestEnv(t, 0)
	prg := parallelProgram(t, env, `items.parallel(x, x > threshold, x)`)
	out := parallelEval(t, prg, map[string]any{
		"items":     []any{1, 2, 3, 4, 5},
		"threshold": 3,
	})
	assertOrderedList(t, out, []ref.Val{types.Int(4), types.Int(5)})
}

// form (2b): range.parallel(v, v2, expr)

func TestParallel_TwoVar_List(t *testing.T) {
	// iterVar = index, iterVar2 = element value.
	env := parallelTestEnv(t, 0)
	prg := parallelProgram(t, env, `items.parallel(i, v, v + "_" + string(i))`)
	out := parallelEval(t, prg, map[string]any{"items": []any{"a", "b", "c"}})
	assertOrderedList(t, out, []ref.Val{
		types.String("a_0"), types.String("b_1"), types.String("c_2"),
	})
}

func TestParallel_TwoVar_List_IndexCorrect(t *testing.T) {
	// The index variable must carry the real position, not always 0.
	env := parallelTestEnv(t, 8)
	prg := parallelProgram(t, env, `items.parallel(i, v, i)`)
	input := make([]any, 50)
	for i := range input {
		input[i] = i * 100
	}
	for run := 0; run < 10; run++ {
		out := parallelEval(t, prg, map[string]any{"items": input})
		list := out.(traits.Lister)
		sz := int(list.Size().(types.Int))
		for j := 0; j < sz; j++ {
			got := list.Get(types.Int(j))
			if got.(types.Int) != types.Int(j) {
				t.Fatalf("run %d index %d: got %v want %d", run, j, got, j)
			}
		}
	}
}

func TestParallel_TwoVar_Map(t *testing.T) {
	// iterVar = key, iterVar2 = value — value directly available without map lookup.
	env := parallelTestEnv(t, 0)
	prg := parallelProgram(t, env, `m.parallel(k, v, v * 100)`)
	out := parallelEval(t, prg, map[string]any{
		"m": map[string]any{"x": 1, "y": 2},
	})
	assertMultiset(t, out, []ref.Val{types.Int(100), types.Int(200)})
}

// assertMultiset checks that val is a CEL list whose elements, treated as a
// multiset, equal want. Used when output order is non-deterministic (map ranges).
func assertMultiset(t *testing.T, val ref.Val, want []ref.Val) {
	t.Helper()
	list, ok := val.(traits.Lister)
	if !ok {
		t.Fatalf("expected list, got %T: %v", val, val)
	}
	sz := int(list.Size().(types.Int))
	if sz != len(want) {
		t.Fatalf("list length: got %d want %d", sz, len(want))
	}
	freq := make(map[string]int, len(want))
	for _, w := range want {
		freq[fmt.Sprint(w)]++
	}
	for i := 0; i < sz; i++ {
		s := fmt.Sprint(list.Get(types.Int(i)))
		if freq[s] == 0 {
			t.Errorf("unexpected element %q in result", s)
		}
		freq[s]--
	}
}

func TestParallel_TwoVar_DistinctVarsRequired(t *testing.T) {
	env := parallelTestEnv(t, 0)
	_, issues := env.Compile(`items.parallel(x, x, x)`)
	if issues == nil || issues.Err() == nil {
		t.Fatal("expected compile error for duplicate iteration variables, got none")
	}
}

// sequential degeneration (perCall == 1)

func TestParallel_Sequential_OneVar(t *testing.T) {
	env := parallelTestEnv(t, 1)
	prg := parallelProgram(t, env, `items.parallel(x, x + 1)`)
	out := parallelEval(t, prg, map[string]any{"items": []any{0, 1, 2}})
	assertOrderedList(t, out, []ref.Val{types.Int(1), types.Int(2), types.Int(3)})
}

func TestParallel_Sequential_Pred(t *testing.T) {
	env := parallelTestEnv(t, 1)
	prg := parallelProgram(t, env, `items.parallel(x, x > 1, x * 10)`)
	out := parallelEval(t, prg, map[string]any{"items": []any{1, 2, 3}})
	assertOrderedList(t, out, []ref.Val{types.Int(20), types.Int(30)})
}

func TestParallel_Sequential_TwoVar(t *testing.T) {
	env := parallelTestEnv(t, 1)
	prg := parallelProgram(t, env, `items.parallel(i, v, string(i) + ":" + v)`)
	out := parallelEval(t, prg, map[string]any{"items": []any{"a", "b"}})
	assertOrderedList(t, out, []ref.Val{types.String("0:a"), types.String("1:b")})
}

// error handling

func TestParallel_Error_Surfaced(t *testing.T) {
	env := parallelTestEnv(t, 0)
	prg := parallelProgram(t, env, `items.parallel(x, 1 / x)`)
	parallelEvalExpectErr(t, prg, map[string]any{"items": []any{1, 0, 2}})
}

func TestParallel_Error_FirstInIterationOrder(t *testing.T) {
	// items[0] and items[2] both produce errors. The call must not panic,
	// deadlock, or return nil, and must return an error deterministically.
	env := parallelTestEnv(t, 4)
	prg := parallelProgram(t, env, `items.parallel(x, 1 / x)`)
	parallelEvalExpectErr(t, prg, map[string]any{"items": []any{0, 1, 0}})
}

func TestParallel_Pred_Error_InPredicate(t *testing.T) {
	env := parallelTestEnv(t, 0)
	prg := parallelProgram(t, env, `items.parallel(x, 1/x > 0, x)`)
	parallelEvalExpectErr(t, prg, map[string]any{"items": []any{1, 0, 2}})
}

func parallelEvalExpectErr(t *testing.T, prg cel.Program, vars map[string]any) {
	t.Helper()
	_, _, err := prg.Eval(vars)
	if err == nil {
		t.Fatal("expected eval error, got nil")
	}
}

// concurrency properties

func TestParallel_ActuallyConcurrent(t *testing.T) {
	// Verify goroutines run concurrently by tracking peak in-flight count.
	var inFlight, peak atomic.Int64

	trackFn := cel.Function("track",
		cel.Overload("track_int",
			[]*cel.Type{cel.IntType},
			cel.IntType,
			cel.UnaryBinding(func(v ref.Val) ref.Val {
				cur := inFlight.Add(1)
				for {
					p := peak.Load()
					if cur <= p || peak.CompareAndSwap(p, cur) {
						break
					}
				}
				// Hold in-flight long enough for goroutines to overlap.
				time.Sleep(time.Millisecond)
				inFlight.Add(-1)
				return v
			}),
		),
	)

	env := parallelTestEnv(t, 8, trackFn)
	prg := parallelProgram(t, env, `items.parallel(x, track(x))`)
	input := make([]any, 32)
	for i := range input {
		input[i] = i
	}
	out := parallelEval(t, prg, map[string]any{"items": input})
	want := make([]ref.Val, 32)
	for i := range want {
		want[i] = types.Int(i)
	}
	assertOrderedList(t, out, want)

	if peak.Load() <= 1 {
		t.Errorf("expected concurrent execution (peak in-flight > 1), got %d", peak.Load())
	}
}

func TestParallel_BoundedConcurrency(t *testing.T) {
	// With perCall=2, no more than 2 goroutines should be in-flight at once.
	var inFlight atomic.Int64
	var overLimit atomic.Bool

	trackFn := cel.Function("track2",
		cel.Overload("track2_int",
			[]*cel.Type{cel.IntType},
			cel.IntType,
			cel.UnaryBinding(func(v ref.Val) ref.Val {
				cur := inFlight.Add(1)
				if cur > 2 {
					overLimit.Store(true)
				}
				ch := make(chan struct{})
				go func() { close(ch) }()
				<-ch
				inFlight.Add(-1)
				return v
			}),
		),
	)

	env := parallelTestEnv(t, 2, trackFn)
	prg := parallelProgram(t, env, `items.parallel(x, track2(x))`)
	input := make([]any, 20)
	for i := range input {
		input[i] = i
	}
	parallelEval(t, prg, map[string]any{"items": input})

	if overLimit.Load() {
		t.Error("concurrency limit violated: more than 2 goroutines in-flight simultaneously")
	}
}

// nesting

func TestParallel_NestedInMap(t *testing.T) {
	env := parallelTestEnv(t, 0)
	prg := parallelProgram(t, env, `outer.map(row, row.parallel(x, x * 2))`)
	out := parallelEval(t, prg, map[string]any{
		"outer": []any{[]any{1, 2}, []any{3, 4}},
	})
	outerList := out.(traits.Lister)
	if int(outerList.Size().(types.Int)) != 2 {
		t.Fatalf("outer size: got %d want 2", outerList.Size())
	}
	assertOrderedList(t, outerList.Get(types.Int(0)), []ref.Val{types.Int(2), types.Int(4)})
	assertOrderedList(t, outerList.Get(types.Int(1)), []ref.Val{types.Int(6), types.Int(8)})
}

func TestParallel_MapNestedInParallel(t *testing.T) {
	env := parallelTestEnv(t, 0)
	prg := parallelProgram(t, env, `outer.parallel(row, row.map(x, x + 1))`)
	out := parallelEval(t, prg, map[string]any{
		"outer": []any{[]any{0, 1}, []any{2, 3}},
	})
	outerList := out.(traits.Lister)
	if int(outerList.Size().(types.Int)) != 2 {
		t.Fatalf("outer size: got %d want 2", outerList.Size())
	}
	assertOrderedList(t, outerList.Get(types.Int(0)), []ref.Val{types.Int(1), types.Int(2)})
	assertOrderedList(t, outerList.Get(types.Int(1)), []ref.Val{types.Int(3), types.Int(4)})
}

func TestParallel_NestedInParallel(t *testing.T) {
	env := parallelTestEnv(t, 4)
	prg := parallelProgram(t, env, `outer.parallel(row, row.parallel(x, x * 2))`)
	out := parallelEval(t, prg, map[string]any{
		"outer": []any{[]any{1, 2, 3}, []any{4, 5, 6}},
	})
	outerList := out.(traits.Lister)
	if int(outerList.Size().(types.Int)) != 2 {
		t.Fatalf("outer size: got %d want 2", outerList.Size())
	}
	assertOrderedList(t, outerList.Get(types.Int(0)), []ref.Val{types.Int(2), types.Int(4), types.Int(6)})
	assertOrderedList(t, outerList.Get(types.Int(1)), []ref.Val{types.Int(8), types.Int(10), types.Int(12)})
}

// compile-time validation

func TestParallel_CompileError_NonIdentIterVar(t *testing.T) {
	env := parallelTestEnv(t, 0)
	_, issues := env.Compile(`items.parallel(1 + 1, x)`)
	if issues == nil || issues.Err() == nil {
		t.Fatal("expected compile error for non-identifier iterVar, got none")
	}
}

func TestParallel_CompileError_AccuVarConflict(t *testing.T) {
	env := parallelTestEnv(t, 0)
	_, issues := env.Compile(`items.parallel(__result__, x)`)
	if issues == nil || issues.Err() == nil {
		t.Fatal("expected compile error for reserved accumulator name, got none")
	}
}

// lib instance isolation

func TestParallel_WrongLibInstance_ReturnsError(t *testing.T) {
	// Compile with lib1, then construct a fresh env with lib2 (different
	// instance) and attempt to program the AST using lib2's env. lib2 has no
	// record of the @parallel sentinel call emitted during lib1's compilation,
	// so its parallelDecorator must return an error.
	lib1 := &parallelLib{perCall: 2}
	lib2 := &parallelLib{perCall: 2}

	env1, err := cel.NewEnv(
		cel.Lib(lib1),
		cel.Variable("items", cel.ListType(cel.DynType)),
	)
	if err != nil {
		t.Fatalf("cel.NewEnv lib1: %v", err)
	}
	ast, issues := env1.Compile(`items.parallel(x, x)`)
	if issues != nil && issues.Err() != nil {
		t.Fatalf("compile: %v", issues.Err())
	}

	// Build a second env with lib2 only. lib2 has no bodies map entry for the
	// @parallel node that lib1's macro emitted, so decoration must fail.
	env2, err := cel.NewEnv(
		cel.Lib(lib2),
		cel.Variable("items", cel.ListType(cel.DynType)),
	)
	if err != nil {
		t.Fatalf("cel.NewEnv lib2: %v", err)
	}
	_, progErr := env2.Program(ast)
	if progErr == nil {
		t.Error("expected error when programming AST compiled by different lib instance, got nil")
	}
}

// activation contract

func TestParallel_HierarchicalActivation(t *testing.T) {
	// The body must see both the iter variable and outer state simultaneously.
	env := parallelTestEnv(t, 4)
	prg := parallelProgram(t, env, `items.parallel(x, x + base)`)
	out := parallelEval(t, prg, map[string]any{
		"items": []any{1, 2, 3},
		"base":  100,
	})
	assertOrderedList(t, out, []ref.Val{types.Int(101), types.Int(102), types.Int(103)})
}

func TestInterpreterActivationContract(t *testing.T) {
	// Confirm our understanding of NewHierarchicalActivation: child takes
	// priority, parent provides fallback, unknown names return not-found.
	child, err := interpreter.NewActivation(map[string]any{"x": 42})
	if err != nil {
		t.Fatalf("NewActivation child: %v", err)
	}
	parent, err := interpreter.NewActivation(map[string]any{"y": 99})
	if err != nil {
		t.Fatalf("NewActivation parent: %v", err)
	}
	hier := interpreter.NewHierarchicalActivation(parent, child)

	if v, ok := hier.ResolveName("x"); !ok || v != 42 {
		t.Errorf("child var x: got (%v, %v) want (42, true)", v, ok)
	}
	if v, ok := hier.ResolveName("y"); !ok || v != 99 {
		t.Errorf("parent var y: got (%v, %v) want (99, true)", v, ok)
	}
	if v, ok := hier.ResolveName("z"); ok {
		t.Errorf("unknown var z: got (%v, %v) want (_, false)", v, ok)
	}
}

// parallelTestEnv builds a cel.Env with Parallel(perCall, 0) and a standard
// set of variable declarations covering all names used in the test suite.
// Additional env options (e.g. custom functions) may be appended via extra.
func parallelTestEnv(t *testing.T, perCall int, extra ...cel.EnvOption) *cel.Env {
	t.Helper()
	base := []cel.EnvOption{
		Parallel(perCall, 0),
		// Scalar inputs used across tests.
		cel.Variable("items", cel.ListType(cel.DynType)),
		cel.Variable("outer", cel.ListType(cel.DynType)),
		cel.Variable("m", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("suffix", cel.StringType),
		cel.Variable("base", cel.IntType),
		cel.Variable("threshold", cel.IntType),
	}
	env, err := cel.NewEnv(append(base, extra...)...)
	if err != nil {
		t.Fatalf("cel.NewEnv: %v", err)
	}
	return env
}

// global cap

func TestParallel_GlobalCap_BoundsTotal(t *testing.T) {
	// With perCall=8 and globalCap=2, no more than 2 leaf bodies run at once.
	var inFlight atomic.Int64
	var overLimit atomic.Bool

	trackFn := cel.Function("track_global",
		cel.Overload("track_global_int",
			[]*cel.Type{cel.IntType},
			cel.IntType,
			cel.UnaryBinding(func(v ref.Val) ref.Val {
				cur := inFlight.Add(1)
				if cur > 2 {
					overLimit.Store(true)
				}
				ch := make(chan struct{})
				go func() { close(ch) }()
				<-ch
				inFlight.Add(-1)
				return v
			}),
		),
	)

	env, err := cel.NewEnv(
		Parallel(8, 2),
		cel.Variable("items", cel.ListType(cel.DynType)),
		trackFn,
	)
	if err != nil {
		t.Fatalf("cel.NewEnv: %v", err)
	}
	prg := parallelProgram(t, env, `items.parallel(x, track_global(x))`)
	input := make([]any, 20)
	for i := range input {
		input[i] = i
	}
	for run := 0; run < 5; run++ {
		overLimit.Store(false)
		parallelEval(t, prg, map[string]any{"items": input})
		if overLimit.Load() {
			t.Fatalf("run %d: global cap violated: more than 2 leaf bodies concurrent", run)
		}
	}
}

func TestParallel_GlobalCap_NestedNoDeadlock(t *testing.T) {
	// Nested parallel with a global cap must not deadlock: the outer parallel's
	// goroutines don't consume global slots (they are not leaves).
	env, err := cel.NewEnv(
		Parallel(4, 3),
		cel.Variable("outer", cel.ListType(cel.DynType)),
	)
	if err != nil {
		t.Fatalf("cel.NewEnv: %v", err)
	}
	prg := parallelProgram(t, env, `outer.parallel(row, row.parallel(x, x * 2))`)

	// Build 8 rows of 4 elements each to create scheduling pressure.
	rows := make([]any, 8)
	for r := range rows {
		row := make([]any, 4)
		for c := range row {
			row[c] = r*4 + c + 1
		}
		rows[r] = row
	}
	for run := 0; run < 10; run++ {
		out := parallelEval(t, prg, map[string]any{"outer": rows})
		outerList := out.(traits.Lister)
		if int(outerList.Size().(types.Int)) != 8 {
			t.Fatalf("run %d: outer size: got %d want 8", run, outerList.Size())
		}
		for r := range rows {
			inner := outerList.Get(types.Int(r))
			want := make([]ref.Val, 4)
			for c := range want {
				want[c] = types.Int((r*4 + c + 1) * 2)
			}
			assertOrderedList(t, inner, want)
		}
	}
}

// assertOrderedList checks that val is a CEL list equal to want in order.
func assertOrderedList(t *testing.T, val ref.Val, want []ref.Val) {
	t.Helper()
	list, ok := val.(traits.Lister)
	if !ok {
		t.Fatalf("expected list, got %T: %v", val, val)
	}
	sz := int(list.Size().(types.Int))
	if sz != len(want) {
		t.Fatalf("list length: got %d want %d (value=%v)", sz, len(want), val)
	}
	for i, w := range want {
		got := list.Get(types.Int(i))
		if got.Equal(w) != types.True {
			t.Errorf("[%d]: got %v want %v", i, got, w)
		}
	}
}

func TestParallel_GlobalCap_ZeroMeansUnlimited(t *testing.T) {
	// globalCap=0 means no global limit; verify actual concurrency.
	var peak atomic.Int64
	var inFlight atomic.Int64

	trackFn := cel.Function("track_unlim",
		cel.Overload("track_unlim_int",
			[]*cel.Type{cel.IntType},
			cel.IntType,
			cel.UnaryBinding(func(v ref.Val) ref.Val {
				cur := inFlight.Add(1)
				for {
					p := peak.Load()
					if cur <= p || peak.CompareAndSwap(p, cur) {
						break
					}
				}
				ch := make(chan struct{})
				go func() { close(ch) }()
				<-ch
				inFlight.Add(-1)
				return v
			}),
		),
	)

	env, err := cel.NewEnv(
		Parallel(8, 0),
		cel.Variable("items", cel.ListType(cel.DynType)),
		trackFn,
	)
	if err != nil {
		t.Fatalf("cel.NewEnv: %v", err)
	}
	prg := parallelProgram(t, env, `items.parallel(x, track_unlim(x))`)
	input := make([]any, 32)
	for i := range input {
		input[i] = i
	}
	parallelEval(t, prg, map[string]any{"items": input})
	if peak.Load() <= 1 {
		t.Errorf("expected concurrent execution (peak > 1), got %d", peak.Load())
	}
}

// race-detector regression

func TestParallel_ConcurrentEvalRace(t *testing.T) {
	// Trip wire for Interpretable.Eval thread-safety. Exercises a variety of
	// Interpretable types under high concurrency so the race detector has the
	// best chance of spotting shared mutable state. This is not a correctness
	// test — existing tests cover that.
	env, err := cel.NewEnv(
		Parallel(8, 0),
		cel.Variable("items", cel.ListType(cel.DynType)),
		cel.Variable("base", cel.IntType),
		cel.Variable("m", cel.MapType(cel.StringType, cel.DynType)),
	)
	if err != nil {
		t.Fatalf("cel.NewEnv: %v", err)
	}
	// Binary ops, function call, list construction, conditional, comprehension, map access.
	src := `items.parallel(x, x * 2 + base + int([x].map(y, y)[0]) + (x > 0 ? x : 0) + m["k"])`
	prg := parallelProgram(t, env, src)
	input := make([]any, 128)
	for i := range input {
		input[i] = i
	}
	vars := map[string]any{
		"items": input,
		"base":  0,
		"m":     map[string]any{"k": 0},
	}
	for iter := 0; iter < 10; iter++ {
		parallelEval(t, prg, vars)
	}
}

func parallelProgram(t *testing.T, env *cel.Env, src string) cel.Program {
	t.Helper()
	ast, issues := env.Compile(src)
	if issues != nil && issues.Err() != nil {
		t.Fatalf("compile %q: %v", src, issues.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("env.Program %q: %v", src, err)
	}
	return prg
}

func parallelEval(t *testing.T, prg cel.Program, vars map[string]any) ref.Val {
	t.Helper()
	out, _, err := prg.Eval(vars)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	return out
}
