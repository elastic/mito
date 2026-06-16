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
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/traits"
)

func TestDecodeLines_Bytes(t *testing.T) {
	env, err := cel.NewEnv(
		Lines(),
		cel.Variable("data", cel.BytesType),
	)
	if err != nil {
		t.Fatalf("failed to create env: %v", err)
	}
	ast, iss := env.Compile(`data.decode_lines()`)
	if iss.Err() != nil {
		t.Fatalf("failed to compile: %v", iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("failed to program: %v", err)
	}

	out, _, err := prg.Eval(map[string]any{"data": []byte("hello\nworld\nfoo\n")})
	if err != nil {
		t.Fatalf("failed to eval: %v", err)
	}

	iter, ok := out.(traits.Iterable)
	if !ok {
		t.Fatalf("expected traits.Iterable, got %T", out)
	}
	it := iter.Iterator()

	want := []string{"hello", "world", "foo"}
	for i, w := range want {
		if it.HasNext() != types.True {
			t.Fatalf("line %d: HasNext() = false, want true", i)
		}
		got := it.Next()
		if got != types.String(w) {
			t.Errorf("line %d: got %v, want %q", i, got, w)
		}
	}
	if it.HasNext() != types.False {
		t.Errorf("expected no more lines")
	}
}

func TestDecodeLines_String(t *testing.T) {
	env, err := cel.NewEnv(
		Lines(),
		cel.Variable("data", cel.StringType),
	)
	if err != nil {
		t.Fatalf("failed to create env: %v", err)
	}
	ast, iss := env.Compile(`data.decode_lines()`)
	if iss.Err() != nil {
		t.Fatalf("failed to compile: %v", iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("failed to program: %v", err)
	}

	out, _, err := prg.Eval(map[string]any{"data": "a\tb\nc\td\n"})
	if err != nil {
		t.Fatalf("failed to eval: %v", err)
	}

	iter, ok := out.(traits.Iterable)
	if !ok {
		t.Fatalf("expected traits.Iterable, got %T", out)
	}
	it := iter.Iterator()

	want := []string{"a\tb", "c\td"}
	for i, w := range want {
		if it.HasNext() != types.True {
			t.Fatalf("line %d: HasNext() = false, want true", i)
		}
		got := it.Next()
		if got != types.String(w) {
			t.Errorf("line %d: got %v, want %q", i, got, w)
		}
	}
	if it.HasNext() != types.False {
		t.Errorf("expected no more lines")
	}
}

func TestDecodeLines_Empty(t *testing.T) {
	env, err := cel.NewEnv(
		Lines(),
		cel.Variable("data", cel.BytesType),
	)
	if err != nil {
		t.Fatalf("failed to create env: %v", err)
	}
	ast, iss := env.Compile(`data.decode_lines()`)
	if iss.Err() != nil {
		t.Fatalf("failed to compile: %v", iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("failed to program: %v", err)
	}

	out, _, err := prg.Eval(map[string]any{"data": []byte("")})
	if err != nil {
		t.Fatalf("failed to eval: %v", err)
	}

	iter, ok := out.(traits.Iterable)
	if !ok {
		t.Fatalf("expected traits.Iterable, got %T", out)
	}
	it := iter.Iterator()
	if it.HasNext() != types.False {
		t.Errorf("expected no lines for empty input")
	}
}

func TestDecodeLines_WithStream(t *testing.T) {
	env, err := cel.NewEnv(
		Lines(),
		Stream(),
		cel.Variable("data", cel.BytesType),
	)
	if err != nil {
		t.Fatalf("failed to create env: %v", err)
	}
	ast, iss := env.Compile(`data.stream_gzip().decode_lines()`)
	if iss.Err() != nil {
		t.Fatalf("failed to compile: %v", iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("failed to program: %v", err)
	}

	gz := gzipBytes(t, []byte("alpha\nbeta\n"))

	out, _, err := prg.Eval(map[string]any{"data": []byte(gz)})
	if err != nil {
		t.Fatalf("failed to eval: %v", err)
	}

	iter, ok := out.(traits.Iterable)
	if !ok {
		t.Fatalf("expected traits.Iterable, got %T", out)
	}
	it := iter.Iterator()

	want := []string{"alpha", "beta"}
	for i, w := range want {
		if it.HasNext() != types.True {
			t.Fatalf("line %d: HasNext() = false, want true", i)
		}
		got := it.Next()
		if got != types.String(w) {
			t.Errorf("line %d: got %v, want %q", i, got, w)
		}
	}
	if it.HasNext() != types.False {
		t.Errorf("expected no more lines")
	}
}

func TestDecodeLines_WithEmit(t *testing.T) {
	emitter := newTestEmitter()
	env, err := cel.NewEnv(
		Lines(),
		Strings(),
		Emit(func() Emitter { return emitter }),
		cel.Variable("data", cel.StringType),
	)
	if err != nil {
		t.Fatalf("failed to create env: %v", err)
	}
	ast, iss := env.Compile(`data.decode_lines().emit(line, {"parts": line.split("\t")})`)
	if iss.Err() != nil {
		t.Fatalf("failed to compile: %v", iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("failed to program: %v", err)
	}

	out, _, err := prg.Eval(map[string]any{"data": "a\t1\nb\t2\n"})
	if err != nil {
		t.Fatalf("failed to eval: %v", err)
	}
	m, ok := out.(traits.Mapper)
	if !ok {
		t.Fatalf("expected traits.Mapper, got %T", out)
	}
	published := m.Get(types.String("published"))
	if published != types.Int(2) {
		t.Errorf("published = %v, want 2", published)
	}
	if len(emitter.values) != 2 {
		t.Fatalf("emitter received %d values, want 2", len(emitter.values))
	}
	row0, ok := emitter.values[0].(map[string]any)
	if !ok {
		t.Fatalf("row 0: got %T, want map[string]any", emitter.values[0])
	}
	parts, ok := row0["parts"].([]any)
	if !ok {
		t.Fatalf("row 0 parts: got %T, want []any", row0["parts"])
	}
	if len(parts) != 2 || parts[0] != "a" || parts[1] != "1" {
		t.Errorf("row 0 parts = %v, want [a 1]", parts)
	}
}
