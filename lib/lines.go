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
	"bufio"
	"bytes"
	"fmt"
	"io"
	"reflect"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// Lines returns a cel.EnvOption to configure the decode_lines function.
//
// # Decode Lines
//
// decode_lines returns a lazy iterable that yields one string per line from
// the receiver. Lines are split by newline; the trailing newline is stripped.
// The receiver may be bytes, string, or a stream value (from stream_gzip or
// stream_zip).
//
//	<bytes>.decode_lines() -> <iterable<string>>
//	<string>.decode_lines() -> <iterable<string>>
//	<stream>.decode_lines() -> <iterable<string>>
//
// Combined with string split functions and emit, this provides a composable
// way to stream delimited text formats:
//
//	// TSV
//	data.stream_gzip().decode_lines().emit(line, line.split("\t"))
//
//	// Pipe-delimited
//	data.decode_lines().emit(line, line.split("|"))
func Lines() cel.EnvOption {
	return cel.Lib(linesLib{})
}

type linesLib struct{}

func (linesLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function("decode_lines",
			cel.MemberOverload(
				"stream_decode_lines",
				[]*cel.Type{streamCELType},
				cel.DynType,
				cel.UnaryBinding(catch(decodeLines)),
			),
			cel.MemberOverload(
				"bytes_decode_lines",
				[]*cel.Type{cel.BytesType},
				cel.DynType,
				cel.UnaryBinding(catch(decodeLines)),
			),
			cel.MemberOverload(
				"string_decode_lines",
				[]*cel.Type{cel.StringType},
				cel.DynType,
				cel.UnaryBinding(catch(decodeLines)),
			),
		),
	}
}

func (linesLib) ProgramOptions() []cel.ProgramOption { return nil }

func decodeLines(val ref.Val) ref.Val {
	var r io.Reader
	switch v := val.(type) {
	case *streamVal:
		r = v.reader
	case types.Bytes:
		r = bytes.NewReader(v)
	case types.String:
		r = bytes.NewReader([]byte(v))
	default:
		return types.NoSuchOverloadErr()
	}
	return &lazyLineStream{scanner: bufio.NewScanner(r)}
}

var (
	_ ref.Val         = (*lazyLineStream)(nil)
	_ traits.Iterable = (*lazyLineStream)(nil)
	_ traits.Iterator = (*lineIterator)(nil)
)

var lazyLineStreamRefType = types.NewObjectType("lazy_line_stream", traits.IterableType)

// lazyLineStream is a ref.Val implementing traits.Iterable that yields
// one string per line from a bufio.Scanner. The scanner is consumed once;
// a second Iterator() call returns an exhausted iterator.
type lazyLineStream struct {
	scanner *bufio.Scanner
}

func (s *lazyLineStream) ConvertToNative(typeDesc reflect.Type) (any, error) {
	return nil, fmt.Errorf("lazy line streams cannot be converted to %v", typeDesc)
}

func (s *lazyLineStream) ConvertToType(typeVal ref.Type) ref.Val {
	if typeVal == types.TypeType {
		return types.NewTypeValue("lazy_line_stream")
	}
	return types.NewErr("type conversion error from 'lazy_line_stream' to '%s'", typeVal.TypeName())
}

func (s *lazyLineStream) Equal(other ref.Val) ref.Val {
	return types.NewErr("lazy line streams are not comparable")
}

func (s *lazyLineStream) Type() ref.Type { return lazyLineStreamRefType }

func (s *lazyLineStream) Value() any { return s.scanner }

func (s *lazyLineStream) Iterator() traits.Iterator {
	return &lineIterator{scanner: s.scanner}
}

// lineIterator wraps a bufio.Scanner as a traits.Iterator.
type lineIterator struct {
	scanner *bufio.Scanner
	scanned bool
	hasMore bool
}

func (it *lineIterator) ConvertToNative(typeDesc reflect.Type) (any, error) {
	return nil, fmt.Errorf("line iterators cannot be converted to %v", typeDesc)
}

func (it *lineIterator) ConvertToType(typeVal ref.Type) ref.Val {
	return types.NewErr("type conversion error from 'line_iterator' to '%s'", typeVal.TypeName())
}

func (it *lineIterator) Equal(other ref.Val) ref.Val {
	return types.NewErr("line iterators are not comparable")
}

func (it *lineIterator) Type() ref.Type { return types.IteratorType }

func (it *lineIterator) Value() any { return it.scanner }

func (it *lineIterator) HasNext() ref.Val {
	if !it.scanned {
		it.hasMore = it.scanner.Scan()
		it.scanned = true
	}
	return types.Bool(it.hasMore)
}

func (it *lineIterator) Next() ref.Val {
	if !it.scanned {
		it.hasMore = it.scanner.Scan()
	}
	it.scanned = false
	if !it.hasMore {
		if err := it.scanner.Err(); err != nil {
			return types.NewErr("decode_lines: %v", err)
		}
		return types.NewErr("decode_lines: no more lines")
	}
	return types.String(it.scanner.Text())
}
