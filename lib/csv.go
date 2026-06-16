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
	"encoding/csv"
	"fmt"
	"io"
	"reflect"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// CSV returns a cel.EnvOption to configure lazy CSV stream decode functions.
//
// # Decode CSV Stream Lazy
//
// decode_csv_stream_lazy returns a lazy iterable that decodes CSV rows on
// demand from the receiver. The first row is treated as a header; each
// subsequent row is returned as a map<string, string> keyed by the header
// values. The receiver may be bytes, string, or a stream value.
//
//	<bytes>.decode_csv_stream_lazy() -> <iterable<map<string, string>>>
//	<string>.decode_csv_stream_lazy() -> <iterable<map<string, string>>>
//	<stream>.decode_csv_stream_lazy() -> <iterable<map<string, string>>>
//
// # Decode CSV Stream Lazy No Header
//
// decode_csv_stream_lazy_no_header returns a lazy iterable that decodes CSV
// rows on demand. Each row is returned as a list<string>. No header row is
// consumed.
//
//	<bytes>.decode_csv_stream_lazy_no_header() -> <iterable<list<string>>>
//	<string>.decode_csv_stream_lazy_no_header() -> <iterable<list<string>>>
//	<stream>.decode_csv_stream_lazy_no_header() -> <iterable<list<string>>>
func CSV() cel.EnvOption {
	return cel.Lib(csvLib{})
}

type csvLib struct{}

func (csvLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function("decode_csv_stream_lazy",
			cel.MemberOverload(
				"stream_decode_csv_stream_lazy",
				[]*cel.Type{streamCELType},
				cel.DynType,
				cel.UnaryBinding(catch(decodeCSVStreamLazy)),
			),
			cel.MemberOverload(
				"bytes_decode_csv_stream_lazy",
				[]*cel.Type{cel.BytesType},
				cel.DynType,
				cel.UnaryBinding(catch(decodeCSVStreamLazy)),
			),
			cel.MemberOverload(
				"string_decode_csv_stream_lazy",
				[]*cel.Type{cel.StringType},
				cel.DynType,
				cel.UnaryBinding(catch(decodeCSVStreamLazy)),
			),
		),

		cel.Function("decode_csv_stream_lazy_no_header",
			cel.MemberOverload(
				"stream_decode_csv_stream_lazy_no_header",
				[]*cel.Type{streamCELType},
				cel.DynType,
				cel.UnaryBinding(catch(decodeCSVStreamLazyNoHeader)),
			),
			cel.MemberOverload(
				"bytes_decode_csv_stream_lazy_no_header",
				[]*cel.Type{cel.BytesType},
				cel.DynType,
				cel.UnaryBinding(catch(decodeCSVStreamLazyNoHeader)),
			),
			cel.MemberOverload(
				"string_decode_csv_stream_lazy_no_header",
				[]*cel.Type{cel.StringType},
				cel.DynType,
				cel.UnaryBinding(catch(decodeCSVStreamLazyNoHeader)),
			),
		),
	}
}

func (csvLib) ProgramOptions() []cel.ProgramOption { return nil }

func decodeCSVStreamLazy(val ref.Val) ref.Val {
	r := csvReader(val)
	if r == nil {
		return types.NoSuchOverloadErr()
	}
	cr := csv.NewReader(r)
	header, err := cr.Read()
	if err != nil {
		return types.NewErr("decode_csv_stream_lazy: reading header: %v", err)
	}
	return &lazyCSVStream{cr: cr, header: header}
}

func decodeCSVStreamLazyNoHeader(val ref.Val) ref.Val {
	r := csvReader(val)
	if r == nil {
		return types.NoSuchOverloadErr()
	}
	return &lazyCSVStream{cr: csv.NewReader(r)}
}

func csvReader(val ref.Val) io.Reader {
	switch v := val.(type) {
	case *streamVal:
		return v.reader
	case types.Bytes:
		return bytes.NewReader(v)
	case types.String:
		return bytes.NewReader([]byte(v))
	default:
		return nil
	}
}

var (
	_ ref.Val         = (*lazyCSVStream)(nil)
	_ traits.Iterable = (*lazyCSVStream)(nil)
	_ traits.Iterator = (*csvStreamIterator)(nil)
)

var lazyCSVStreamRefType = types.NewObjectType("lazy_csv_stream", traits.IterableType)

// lazyCSVStream is a ref.Val implementing traits.Iterable that decodes CSV
// rows on demand from a csv.Reader. If header is non-nil, each row is
// returned as map[string]string; otherwise as []string.
type lazyCSVStream struct {
	cr     *csv.Reader
	header []string
}

func (s *lazyCSVStream) ConvertToNative(typeDesc reflect.Type) (any, error) {
	return nil, fmt.Errorf("lazy CSV streams cannot be converted to %v", typeDesc)
}

func (s *lazyCSVStream) ConvertToType(typeVal ref.Type) ref.Val {
	if typeVal == types.TypeType {
		return types.NewTypeValue("lazy_csv_stream")
	}
	return types.NewErr("type conversion error from 'lazy_csv_stream' to '%s'", typeVal.TypeName())
}

func (s *lazyCSVStream) Equal(other ref.Val) ref.Val {
	return types.NewErr("lazy CSV streams are not comparable")
}

func (s *lazyCSVStream) Type() ref.Type { return lazyCSVStreamRefType }

func (s *lazyCSVStream) Value() any { return s.cr }

func (s *lazyCSVStream) Iterator() traits.Iterator {
	return &csvStreamIterator{cr: s.cr, header: s.header}
}

// csvStreamIterator wraps a csv.Reader as a traits.Iterator.
type csvStreamIterator struct {
	cr     *csv.Reader
	header []string

	// next holds a prefetched row; hasNext reads ahead one row
	// because csv.Reader has no equivalent of json.Decoder.More().
	next []string
	done bool
	err  error
}

func (it *csvStreamIterator) ConvertToNative(typeDesc reflect.Type) (any, error) {
	return nil, fmt.Errorf("CSV stream iterators cannot be converted to %v", typeDesc)
}

func (it *csvStreamIterator) ConvertToType(typeVal ref.Type) ref.Val {
	return types.NewErr("type conversion error from 'csv_stream_iterator' to '%s'", typeVal.TypeName())
}

func (it *csvStreamIterator) Equal(other ref.Val) ref.Val {
	return types.NewErr("CSV stream iterators are not comparable")
}

func (it *csvStreamIterator) Type() ref.Type { return types.IteratorType }

func (it *csvStreamIterator) Value() any { return it.cr }

func (it *csvStreamIterator) HasNext() ref.Val {
	if it.done {
		return types.False
	}
	if it.next != nil {
		return types.True
	}
	rec, err := it.cr.Read()
	if err != nil {
		it.done = true
		if err != io.EOF {
			it.err = err
		}
		return types.False
	}
	it.next = rec
	return types.True
}

func (it *csvStreamIterator) Next() ref.Val {
	rec := it.next
	it.next = nil
	if rec == nil {
		if it.err != nil {
			return types.NewErr("csv: %v", it.err)
		}
		return types.NewErr("csv: no more rows")
	}
	if it.header != nil {
		m := make(map[string]string, len(it.header))
		for j, name := range it.header {
			if j < len(rec) {
				m[name] = rec[j]
			}
		}
		return types.DefaultTypeAdapter.NativeToValue(m)
	}
	return types.DefaultTypeAdapter.NativeToValue(rec)
}
