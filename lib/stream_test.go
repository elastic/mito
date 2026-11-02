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
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"
	"math"
	"strings"
	"testing"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
	"cel.dev/cel-go/common/types/traits"
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

// bzip2Fixture is "{\"a\":1}\n{\"b\":2}\n" compressed with bzip2.
const bzip2Fixture = "QlpoOTFBWSZTWQ/l9WEAAAZZgAAQEAAwEDAAAAogADEGTECUmh6eokxEgQXRdyRThQkA/l9WEA=="

// tarBzip2Fixture is a tar.bz2 holding a directory d/ with two bzip2 files:
// d/m1.json.bz2 (bzip2Fixture) and d/m2.json.bz2 (holding "{\"c\":3}\n").
const tarBzip2Fixture = "QlpoOTFBWSZTWVN2mt4AAJt/98vx4IHXgf/gdhRJMfZTnpBiAMQREAABAGAAAgAGCTAA01g0qaNAAA00AAAAZGgPUAMnhQSlT9JPU9TEJhMmhppo00MjJ+qA9EMTAQxAypPSBkAMhoAANABoGTQaAArlbiOXeVE4FCs5AJT57oEQGyjKWLM6JrCOeJRmWlSZEkSAU1EXkU+nQMCNfXMpaHvaVJY706NXhRSyY0cDlXEYPB0UlmyqJUkkqpAk6xtQAYISKZq5EOphGcPHlLc1jRY+m8yAhGLAzDkPeE3C8fi/7wwkhJlr/A+az5HKyLEr4RgieiP6C4MPodBi8Oh4oBOEn8Gg9SO/OdCE1BnSsSnxF/F3JFOFCQU3aa3g"

func decodeB64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type tarEntry struct {
	name string
	typ  byte
	body string
}

func tarBytes(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: 0o644}
		h.Size = int64(len(e.body))
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func readStream(t *testing.T, v ref.Val) string {
	t.Helper()
	sv, ok := v.(*streamVal)
	if !ok {
		t.Fatalf("expected *streamVal, got %T: %v", v, v)
	}
	b, err := io.ReadAll(sv.reader)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(b)
}

func TestStreamBzip2(t *testing.T) {
	got := readStream(t, streamBzip2(types.Bytes(decodeB64(t, bzip2Fixture))))
	want := "{\"a\":1}\n{\"b\":2}\n"
	if got != want {
		t.Errorf("stream_bzip2 = %q; want %q", got, want)
	}
}

func TestStreamBzip2_Invalid(t *testing.T) {
	for _, in := range []string{"", "BZ", "not bzip2 data", "BZh0", "BZhx"} {
		v := streamBzip2(types.Bytes(in))
		if !types.IsError(v) {
			t.Errorf("stream_bzip2(%q) = %v; want error", in, v)
		}
	}
}

func TestStreamBzip2_TruncatedData(t *testing.T) {
	data := decodeB64(t, bzip2Fixture)
	v := streamBzip2(types.Bytes(data[:len(data)/2]))
	sv, ok := v.(*streamVal)
	if !ok {
		t.Fatalf("expected *streamVal, got %T: %v", v, v)
	}
	_, err := io.ReadAll(sv.reader)
	if err == nil {
		t.Fatal("expected error reading truncated bzip2 data")
	}
	if !strings.HasPrefix(err.Error(), "stream_bzip2: ") {
		t.Errorf("error = %q; want stream_bzip2 prefix", err)
	}
}

func TestStreamErrorPrefixes(t *testing.T) {
	archive := tarBytes(t, []tarEntry{{"a", tar.TypeReg, "0123456789"}})
	cut := archive[:512+5] // Header and half of the entry data.
	gz := gzipBytes(t, []byte("0123456789"))
	tests := []struct {
		name   string
		stream ref.Val
		prefix string
	}{
		{"gzip", streamGzip(types.Bytes(gz[:len(gz)-12])), "stream_gzip: "},
		{"tar_entry", streamTarEntry(types.Bytes(cut), types.Int(0)), "stream_tar: "},
		{"tar_all", streamTarAll(types.Bytes(cut)), "stream_tar: "},
	}
	for _, test := range tests {
		sv, ok := test.stream.(*streamVal)
		if !ok {
			t.Errorf("%s: expected *streamVal, got %T: %v", test.name, test.stream, test.stream)
			continue
		}
		_, err := io.ReadAll(sv.reader)
		if err == nil || !strings.HasPrefix(err.Error(), test.prefix) {
			t.Errorf("%s: read error = %v; want prefix %q", test.name, err, test.prefix)
		}
	}
}

func TestRegularFile(t *testing.T) {
	tests := []struct {
		typ  byte
		want bool
	}{
		{tar.TypeReg, true},
		{tar.TypeCont, true},
		{tar.TypeGNUSparse, true},
		{tar.TypeDir, false},
		{tar.TypeSymlink, false},
		{tar.TypeLink, false},
		{tar.TypeChar, false},
		{tar.TypeBlock, false},
		{tar.TypeFifo, false},
		{tar.TypeXGlobalHeader, false},
	}
	for _, test := range tests {
		got := regularFile(&tar.Header{Typeflag: test.typ})
		if got != test.want {
			t.Errorf("regularFile(%q) = %t; want %t", test.typ, got, test.want)
		}
	}
}

func TestStreamTar(t *testing.T) {
	data := tarBytes(t, []tarEntry{
		{"d/", tar.TypeDir, ""},
		{"d/a", tar.TypeReg, "one\n"},
		{"l", tar.TypeSymlink, ""},
		{"c", tar.TypeCont, "cont\n"},
		{"d/b", tar.TypeReg, "two\n"},
	})
	for i, want := range []string{"one\n", "cont\n", "two\n"} {
		got := readStream(t, streamTarEntry(types.Bytes(data), types.Int(i)))
		if got != want {
			t.Errorf("stream_tar(%d) = %q; want %q", i, got, want)
		}
	}
	for _, i := range []int64{-1, 3, 1 << 32, math.MaxInt64} {
		if v := streamTarEntry(types.Bytes(data), types.Int(i)); !types.IsError(v) {
			t.Errorf("stream_tar(%d) = %v; want error", i, v)
		}
	}
	got := readStream(t, streamTarAll(types.Bytes(data)))
	if want := "one\ncont\ntwo\n"; got != want {
		t.Errorf("stream_tar() = %q; want %q", got, want)
	}
}

func TestStreamTar_EmptyEntries(t *testing.T) {
	data := tarBytes(t, []tarEntry{
		{"a", tar.TypeReg, ""},
		{"b", tar.TypeReg, "x"},
		{"c", tar.TypeReg, ""},
		{"d", tar.TypeReg, "y"},
	})
	got := readStream(t, streamTarAll(types.Bytes(data)))
	if want := "xy"; got != want {
		t.Errorf("stream_tar() = %q; want %q", got, want)
	}
}

func TestStreamTar_Invalid(t *testing.T) {
	if v := streamTarEntry(types.Bytes(strings.Repeat("x", 600)), types.Int(0)); !types.IsError(v) {
		t.Errorf("expected error for invalid tar, got %v", v)
	}
}

func TestStreamGzip_FromStream(t *testing.T) {
	inner := gzipBytes(t, []byte("hello"))
	data := tarBytes(t, []tarEntry{{"a.gz", tar.TypeReg, string(inner)}})
	got := readStream(t, streamGzip(streamTarEntry(types.Bytes(data), types.Int(0))))
	if want := "hello"; got != want {
		t.Errorf("stream_gzip = %q; want %q", got, want)
	}
}

func TestStreamTarBzip2_Chain(t *testing.T) {
	env, err := cel.NewEnv(
		JSON(nil),
		Stream(),
		cel.Variable("body", cel.BytesType),
	)
	if err != nil {
		t.Fatalf("cel.NewEnv: %v", err)
	}
	ast, issues := env.Compile(`body.stream_bzip2().stream_tar().stream_bzip2().decode_json_stream_lazy().map(x, x)`)
	if issues != nil && issues.Err() != nil {
		t.Fatalf("compile: %v", issues.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("env.Program: %v", err)
	}
	out, _, err := prg.Eval(map[string]any{"body": decodeB64(t, tarBzip2Fixture)})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	list, ok := out.(traits.Lister)
	if !ok {
		t.Fatalf("expected Lister, got %T", out)
	}
	if list.Size() != types.Int(3) {
		t.Errorf("got %v records; want 3: %v", list.Size(), out)
	}
}
