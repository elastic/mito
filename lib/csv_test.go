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
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

func TestDecodeCSVStreamLazy_Header(t *testing.T) {
	env, err := cel.NewEnv(
		CSV(),
		cel.Variable("data", cel.BytesType),
	)
	if err != nil {
		t.Fatalf("failed to create env: %v", err)
	}
	ast, iss := env.Compile(`data.decode_csv_stream_lazy()`)
	if iss.Err() != nil {
		t.Fatalf("failed to compile: %v", iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("failed to program: %v", err)
	}

	csv := []byte("name,age\nalice,30\nbob,25\n")
	out, _, err := prg.Eval(map[string]any{"data": csv})
	if err != nil {
		t.Fatalf("failed to eval: %v", err)
	}

	iter, ok := out.(traits.Iterable)
	if !ok {
		t.Fatalf("expected traits.Iterable, got %T", out)
	}
	it := iter.Iterator()

	want := []map[string]string{
		{"name": "alice", "age": "30"},
		{"name": "bob", "age": "25"},
	}
	for i, w := range want {
		if it.HasNext() != types.True {
			t.Fatalf("row %d: HasNext() = false, want true", i)
		}
		row := it.Next()
		m, ok := row.(traits.Mapper)
		if !ok {
			t.Fatalf("row %d: got %T, want traits.Mapper", i, row)
		}
		for k, v := range w {
			got := m.Get(types.String(k))
			if got != types.String(v) {
				t.Errorf("row %d: %s = %v, want %q", i, k, got, v)
			}
		}
	}
	if it.HasNext() != types.False {
		t.Errorf("expected no more rows")
	}
}

func TestDecodeCSVStreamLazy_NoHeader(t *testing.T) {
	env, err := cel.NewEnv(
		CSV(),
		cel.Variable("data", cel.BytesType),
	)
	if err != nil {
		t.Fatalf("failed to create env: %v", err)
	}
	ast, iss := env.Compile(`data.decode_csv_stream_lazy_no_header()`)
	if iss.Err() != nil {
		t.Fatalf("failed to compile: %v", iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("failed to program: %v", err)
	}

	csv := []byte("alice,30\nbob,25\n")
	out, _, err := prg.Eval(map[string]any{"data": csv})
	if err != nil {
		t.Fatalf("failed to eval: %v", err)
	}

	iter, ok := out.(traits.Iterable)
	if !ok {
		t.Fatalf("expected traits.Iterable, got %T", out)
	}
	it := iter.Iterator()

	want := [][]string{
		{"alice", "30"},
		{"bob", "25"},
	}
	for i, w := range want {
		if it.HasNext() != types.True {
			t.Fatalf("row %d: HasNext() = false, want true", i)
		}
		row := it.Next()
		l, ok := row.(traits.Lister)
		if !ok {
			t.Fatalf("row %d: got %T, want traits.Lister", i, row)
		}
		for j, v := range w {
			got := l.Get(types.Int(j))
			if got != types.String(v) {
				t.Errorf("row %d col %d: got %v, want %q", i, j, got, v)
			}
		}
	}
	if it.HasNext() != types.False {
		t.Errorf("expected no more rows")
	}
}

func TestDecodeCSVStreamLazy_WithEmit(t *testing.T) {
	emitter := newTestEmitter()
	env, err := cel.NewEnv(
		CSV(),
		Emit(func() Emitter { return emitter }),
		cel.Variable("data", cel.BytesType),
	)
	if err != nil {
		t.Fatalf("failed to create env: %v", err)
	}
	ast, iss := env.Compile(`data.decode_csv_stream_lazy().emit(row, row)`)
	if iss.Err() != nil {
		t.Fatalf("failed to compile: %v", iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("failed to program: %v", err)
	}

	csv := []byte("name,value\nfoo,1\nbar,2\nbaz,3\n")
	out, _, err := prg.Eval(map[string]any{"data": csv})
	if err != nil {
		t.Fatalf("failed to eval: %v", err)
	}
	m, ok := out.(traits.Mapper)
	if !ok {
		t.Fatalf("expected traits.Mapper, got %T", out)
	}
	published := m.Get(types.String("published"))
	if published != types.Int(3) {
		t.Errorf("published = %v, want 3", published)
	}
	if len(emitter.values) != 3 {
		t.Fatalf("emitter received %d values, want 3", len(emitter.values))
	}
	row0, ok := emitter.values[0].(map[string]any)
	if !ok {
		t.Fatalf("row 0: got %T, want map[string]any", emitter.values[0])
	}
	if row0["name"] != "foo" || row0["value"] != "1" {
		t.Errorf("row 0 = %v, want {name:foo value:1}", row0)
	}
}

func TestDecodeCSVStreamLazy_Stream(t *testing.T) {
	env, err := cel.NewEnv(
		CSV(),
		Stream(),
		cel.Variable("data", cel.BytesType),
	)
	if err != nil {
		t.Fatalf("failed to create env: %v", err)
	}
	ast, iss := env.Compile(`data.stream_gzip().decode_csv_stream_lazy()`)
	if iss.Err() != nil {
		t.Fatalf("failed to compile: %v", iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("failed to program: %v", err)
	}

	csvData := []byte("name,age\nalice,30\n")
	gz := gzipBytes(t, csvData)

	out, _, err := prg.Eval(map[string]any{"data": []byte(gz)})
	if err != nil {
		t.Fatalf("failed to eval: %v", err)
	}
	iter, ok := out.(traits.Iterable)
	if !ok {
		t.Fatalf("expected traits.Iterable, got %T", out)
	}
	it := iter.Iterator()
	if it.HasNext() != types.True {
		t.Fatal("expected at least one row")
	}
	row := it.Next()
	m, ok := row.(traits.Mapper)
	if !ok {
		t.Fatalf("got %T, want traits.Mapper", row)
	}
	if got := m.Get(types.String("name")); got != types.String("alice") {
		t.Errorf("name = %v, want alice", got)
	}
}

func TestDecodeCSVStreamLazy_EmptyHeader(t *testing.T) {
	env, err := cel.NewEnv(
		CSV(),
		cel.Variable("data", cel.BytesType),
	)
	if err != nil {
		t.Fatalf("failed to create env: %v", err)
	}
	ast, iss := env.Compile(`data.decode_csv_stream_lazy()`)
	if iss.Err() != nil {
		t.Fatalf("failed to compile: %v", iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("failed to program: %v", err)
	}

	_, _, err = prg.Eval(map[string]any{"data": []byte("")})
	if err == nil {
		t.Error("expected error for empty input, got nil")
	}
}

// gzipBytes is defined in stream_test.go but we need it here too.
// Since both files are in the same package, it's shared.
func csvCheckRef(v ref.Val) {
	// Compile-time interface checks for test coverage.
	var _ ref.Val = (*lazyCSVStream)(nil)
	var _ traits.Iterable = (*lazyCSVStream)(nil)
	var _ traits.Iterator = (*csvStreamIterator)(nil)
}
