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
	"errors"
	"sync"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

func TestEmit_TwoArg_Basic(t *testing.T) {
	emitter := newTestEmitter()
	env := emitTestEnv(t, emitter)
	prg := emitProgram(t, env, `items.emit(x, x)`)
	out := emitEval(t, prg, map[string]any{"items": []any{1, 2, 3}})
	m, ok := out.(traits.Mapper)
	if !ok {
		t.Fatalf("expected map, got %T", out)
	}
	published := m.Get(types.String("published"))
	if published != types.Int(3) {
		t.Errorf("published = %v, want 3", published)
	}
	if len(emitter.values) != 3 {
		t.Errorf("emitter received %d values, want 3", len(emitter.values))
	}
	// Two-arg form: no cursor field in result.
	cursorField := m.Get(types.String("cursor"))
	if !types.IsError(cursorField) {
		t.Errorf("expected no cursor field in two-arg form, got %v", cursorField)
	}
}

func TestEmit_ThreeArg_WithCursor(t *testing.T) {
	emitter := newTestEmitter()
	env := emitTestEnv(t, emitter)
	prg := emitProgram(t, env, `items.emit(x, {"value": x}, {"offset": x})`)
	out := emitEval(t, prg, map[string]any{"items": []any{10, 20, 30}})
	m, ok := out.(traits.Mapper)
	if !ok {
		t.Fatalf("expected map, got %T", out)
	}
	published := m.Get(types.String("published"))
	if published != types.Int(3) {
		t.Errorf("published = %v, want 3", published)
	}
	cursor := m.Get(types.String("cursor"))
	if types.IsError(cursor) {
		t.Fatalf("expected cursor field, got error: %v", cursor)
	}
	// Last cursor should be {"offset": 30}.
	cm, ok := cursor.(traits.Mapper)
	if !ok {
		t.Fatalf("expected cursor map, got %T", cursor)
	}
	offset := cm.Get(types.String("offset"))
	if offset != types.Int(30) {
		t.Errorf("cursor.offset = %v, want 30", offset)
	}
}

func TestEmit_EmptyRange(t *testing.T) {
	emitter := newTestEmitter()
	env := emitTestEnv(t, emitter)
	prg := emitProgram(t, env, `items.emit(x, x, {"n": x})`)
	out := emitEval(t, prg, map[string]any{"items": []any{}})
	m, ok := out.(traits.Mapper)
	if !ok {
		t.Fatalf("expected map, got %T", out)
	}
	published := m.Get(types.String("published"))
	if published != types.Int(0) {
		t.Errorf("published = %v, want 0", published)
	}
	// No cursor field when zero events published.
	cursorField := m.Get(types.String("cursor"))
	if !types.IsError(cursorField) {
		t.Errorf("expected no cursor field for empty range, got %v", cursorField)
	}
}

func TestEmit_PartialFailure(t *testing.T) {
	emitter := newTestEmitter()
	emitter.failAt = 2

	env := emitTestEnv(t, emitter)
	prg := emitProgram(t, env, `items.emit(x, x, {"n": x})`)
	out := emitEval(t, prg, map[string]any{"items": []any{1, 2, 3, 4, 5}})
	m, ok := out.(traits.Mapper)
	if !ok {
		t.Fatalf("expected map, got %T", out)
	}
	published := m.Get(types.String("published"))
	if published != types.Int(2) {
		t.Errorf("published = %v, want 2", published)
	}
	cv := m.Get(types.String("cursor"))
	cm, ok := cv.(traits.Mapper)
	if !ok {
		t.Fatalf("cursor: got %T, want traits.Mapper", cv)
	}
	n := cm.Get(types.String("n"))
	if n != types.Int(2) {
		t.Errorf("cursor.n = %v, want 2", n)
	}
	if len(emitter.values) != 2 {
		t.Errorf("emitter received %d values, want 2", len(emitter.values))
	}
	errVal := m.Get(types.String("error"))
	errStr, ok := errVal.(types.String)
	if !ok {
		t.Fatalf("error: got %T, want types.String", errVal)
	}
	if errStr != types.String("emit failed") {
		t.Errorf("error = %v, want %q", errStr, "emit failed")
	}
}

func TestEmit_WithLazyStream(t *testing.T) {
	emitter := newTestEmitter()
	env := emitTestEnv(t, emitter)
	prg := emitProgram(t, env, `data.decode_json_stream_lazy().emit(x, x)`)
	ndjson := []byte("{\"a\":1}\n{\"b\":2}\n{\"c\":3}\n")
	out := emitEval(t, prg, map[string]any{"data": ndjson})
	m, ok := out.(traits.Mapper)
	if !ok {
		t.Fatalf("expected map, got %T", out)
	}
	published := m.Get(types.String("published"))
	if published != types.Int(3) {
		t.Errorf("published = %v, want 3", published)
	}
	if len(emitter.values) != 3 {
		t.Errorf("emitter received %d values, want 3", len(emitter.values))
	}
}

func TestEmit_WithGzipStream(t *testing.T) {
	emitter := newTestEmitter()
	env := emitTestEnv(t, emitter)
	prg := emitProgram(t, env, `data.stream_gzip().decode_json_stream_lazy().emit(x, x)`)
	ndjson := "{\"a\":1}\n{\"b\":2}\n"
	compressed := gzipBytes(t, []byte(ndjson))
	out := emitEval(t, prg, map[string]any{"data": compressed})
	m, ok := out.(traits.Mapper)
	if !ok {
		t.Fatalf("expected map, got %T", out)
	}
	published := m.Get(types.String("published"))
	if published != types.Int(2) {
		t.Errorf("published = %v, want 2", published)
	}
}

func TestEmit_CompileError_NonIdentIterVar(t *testing.T) {
	emitter := newTestEmitter()
	env := emitTestEnv(t, emitter)
	_, issues := env.Compile(`items.emit(1 + 1, x)`)
	if issues == nil || issues.Err() == nil {
		t.Fatal("expected compile error for non-identifier iterVar")
	}
}

func TestEmit_WrongLibInstance(t *testing.T) {
	lib1 := &emitLib{factory: func() Emitter { return newTestEmitter() }}
	lib2 := &emitLib{factory: func() Emitter { return newTestEmitter() }}

	env1, err := cel.NewEnv(
		cel.Lib(lib1),
		cel.Variable("items", cel.ListType(cel.DynType)),
	)
	if err != nil {
		t.Fatalf("cel.NewEnv lib1: %v", err)
	}
	ast, issues := env1.Compile(`items.emit(x, x)`)
	if issues != nil && issues.Err() != nil {
		t.Fatalf("compile: %v", issues.Err())
	}

	env2, err := cel.NewEnv(
		cel.Lib(lib2),
		cel.Variable("items", cel.ListType(cel.DynType)),
	)
	if err != nil {
		t.Fatalf("cel.NewEnv lib2: %v", err)
	}
	_, progErr := env2.Program(ast)
	if progErr == nil {
		t.Error("expected error when programming AST compiled by different lib instance")
	}
}

type testEmitter struct {
	mu      sync.Mutex
	values  []any
	cursors []any
	failAt  int // -1 to never fail
}

func newTestEmitter() *testEmitter {
	return &testEmitter{failAt: -1}
}

func (e *testEmitter) Emit(value, cursor any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.failAt >= 0 && len(e.values) >= e.failAt {
		return errors.New("emit failed")
	}
	e.values = append(e.values, value)
	e.cursors = append(e.cursors, cursor)
	return nil
}

func emitTestEnv(t *testing.T, emitter *testEmitter, extra ...cel.EnvOption) *cel.Env {
	t.Helper()
	base := []cel.EnvOption{
		Emit(func() Emitter { return emitter }),
		JSON(nil),
		Stream(),
		cel.Variable("items", cel.ListType(cel.DynType)),
		cel.Variable("data", cel.BytesType),
	}
	env, err := cel.NewEnv(append(base, extra...)...)
	if err != nil {
		t.Fatalf("cel.NewEnv: %v", err)
	}
	return env
}

func emitProgram(t *testing.T, env *cel.Env, src string) cel.Program {
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

func emitEval(t *testing.T, prg cel.Program, vars map[string]any) ref.Val {
	t.Helper()
	out, _, err := prg.Eval(vars)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	return out
}
