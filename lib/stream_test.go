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
	"bytes"
	"compress/gzip"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

func TestStreamGzip_LazyDecode(t *testing.T) {
	ndjson := "{\"a\":1}\n{\"b\":2}\n{\"c\":3}\n"
	compressed := gzipBytes(t, []byte(ndjson))

	env, err := cel.NewEnv(
		JSON(nil),
		Stream(),
		cel.Variable("body", cel.BytesType),
	)
	if err != nil {
		t.Fatalf("cel.NewEnv: %v", err)
	}
	ast, issues := env.Compile(`body.stream_gzip().decode_json_stream_lazy()`)
	if issues != nil && issues.Err() != nil {
		t.Fatalf("compile: %v", issues.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("env.Program: %v", err)
	}
	out, _, err := prg.Eval(map[string]any{"body": []byte(compressed)})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	iter, ok := out.(traits.Iterable)
	if !ok {
		t.Fatalf("expected Iterable, got %T", out)
	}
	it := iter.Iterator()
	var results []ref.Val
	for it.HasNext() == types.True {
		results = append(results, it.Next())
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
}

func TestStreamGzip_TypeProperties(t *testing.T) {
	data := gzipBytes(t, []byte("hello"))
	s := streamGzip(types.Bytes(data))
	sv, ok := s.(*streamVal)
	if !ok {
		t.Fatalf("expected *streamVal, got %T: %v", s, s)
	}
	if sv.Type().TypeName() != "stream" {
		t.Errorf("TypeName = %q, want %q", sv.Type().TypeName(), "stream")
	}
	if !types.IsError(sv.Equal(sv)) {
		t.Error("Equal should return an error for streams")
	}
}

func TestStreamZip_OutOfRange(t *testing.T) {
	result := streamZip(types.Bytes{0x50, 0x4b}, types.Int(0))
	if !types.IsError(result) {
		t.Fatalf("expected error for invalid zip, got %T", result)
	}
}

func TestLazyJSONStream_BytesFallback(t *testing.T) {
	env, err := cel.NewEnv(
		JSON(nil),
		Stream(),
		cel.Variable("data", cel.BytesType),
	)
	if err != nil {
		t.Fatalf("cel.NewEnv: %v", err)
	}
	ast, issues := env.Compile(`data.decode_json_stream_lazy()`)
	if issues != nil && issues.Err() != nil {
		t.Fatalf("compile: %v", issues.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("env.Program: %v", err)
	}
	ndjson := []byte("{\"x\":1}{\"y\":2}")
	out, _, err := prg.Eval(map[string]any{"data": ndjson})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	iter, ok := out.(traits.Iterable)
	if !ok {
		t.Fatalf("expected Iterable, got %T", out)
	}
	it := iter.Iterator()
	count := 0
	for it.HasNext() == types.True {
		it.Next()
		count++
	}
	if count != 2 {
		t.Errorf("got %d elements, want 2", count)
	}
}

func TestLazyJSONStream_InComprehension(t *testing.T) {
	env, err := cel.NewEnv(
		JSON(nil),
		Stream(),
		cel.Variable("data", cel.BytesType),
	)
	if err != nil {
		t.Fatalf("cel.NewEnv: %v", err)
	}
	ast, issues := env.Compile(`data.decode_json_stream_lazy().map(x, x)`)
	if issues != nil && issues.Err() != nil {
		t.Fatalf("compile: %v", issues.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("env.Program: %v", err)
	}
	ndjson := []byte("{\"a\":1}\n{\"b\":2}\n{\"c\":3}\n")
	out, _, err := prg.Eval(map[string]any{"data": ndjson})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	list, ok := out.(traits.Lister)
	if !ok {
		t.Fatalf("expected Lister, got %T", out)
	}
	if list.Size() != types.Int(3) {
		t.Errorf("list size = %v, want 3", list.Size())
	}
}

func TestLazyJSONStream_UseNumber(t *testing.T) {
	env, err := cel.NewEnv(
		JSON(nil),
		Stream(),
		cel.Variable("data", cel.BytesType),
	)
	if err != nil {
		t.Fatalf("cel.NewEnv: %v", err)
	}
	ast, issues := env.Compile(`data.decode_json_stream_lazy_string_numbers().map(x, x)`)
	if issues != nil && issues.Err() != nil {
		t.Fatalf("compile: %v", issues.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("env.Program: %v", err)
	}
	out, _, err := prg.Eval(map[string]any{"data": []byte(`{"n":9007199254740993}`)})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	list, ok := out.(traits.Lister)
	if !ok {
		t.Fatalf("expected Lister, got %T", out)
	}
	if list.Size() != types.Int(1) {
		t.Fatalf("list size = %v, want 1", list.Size())
	}
	elem := list.Get(types.Int(0))
	m, ok := elem.(traits.Mapper)
	if !ok {
		t.Fatalf("expected Mapper, got %T", elem)
	}
	n := m.Get(types.String("n"))
	if n.Value() != "9007199254740993" {
		t.Errorf("number = %v (%T), want string %q", n.Value(), n.Value(), "9007199254740993")
	}
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
