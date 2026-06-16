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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"

	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// JSON returns a cel.EnvOption to configure extended functions for JSON
// coding and decoding. The parameter specifies the CEL type adapter to use.
// A nil adapter is valid an will give an option using the default type
// adapter, types.DefaultTypeAdapter.
//
// # Encode JSON
//
// encode_json returns a string of the JSON encoding of the receiver or
// parameter:
//
//	encode_json(<dyn>) -> <string>
//	<dyn>.encode_json() -> <string>
//
// Examples:
//
//	{"a":1, "b":[1, 2, 3]}.encode_json()  // return "{\"a\":1,\"b\":[1,2,3]}"
//	encode_json({"a":1, "b":[1, 2, 3]})   // return "{\"a\":1,\"b\":[1,2,3]}"
//
// # Decode JSON
//
// decode_json returns the object described by the JSON encoding of the receiver
// or parameter:
//
//	<bytes>.decode_json() -> <dyn>
//	<string>.decode_json() -> <dyn>
//	decode_json(<bytes>) -> <dyn>
//	decode_json(<string>) -> <dyn>
//
// Examples:
//
//	"{\"a\":1,\"b\":[1,2,3]}".decode_json()   // return {"a":1, "b":[1, 2, 3]}
//	b"{\"a\":1,\"b\":[1,2,3]}".decode_json()  // return {"a":1, "b":[1, 2, 3]}
//
// decode_json_string_numbers returns the object described by the JSON encoding
// of the receiver or parameter except that numbers will be represented as the
// literal string syntax used to represent them:
//
//	<bytes>.decode_json_string_numbers() -> <dyn>
//	<string>.decode_json_string_numbers() -> <dyn>
//	decode_json_string_numbers(<bytes>) -> <dyn>
//	decode_json_string_numbers(<string>) -> <dyn>
//
// Examples:
//
//	"{\"a\":1,\"b\":[1,2,3]}".decode_json_string_numbers()   // return {"a":"1", "b":["1", "2", "3"]}
//	b"{\"a\":1,\"b\":[1,2,3]}".decode_json_string_numbers()  // return {"a":"1", "b":["1", "2", "3"]}
//
// # Decode JSON Stream
//
// decode_json_stream returns a list of objects described by the JSON stream
// of the receiver or parameter:
//
//	<bytes>.decode_json_stream() -> <list<dyn>>
//	<string>.decode_json_stream() -> <list<dyn>>
//	decode_json_stream(<bytes>) -> <list<dyn>>
//	decode_json_stream(<string>) -> <list<dyn>>
//
// Examples:
//
//	'{"a":1}{"b":2}'.decode_json_stream()   // return [{"a":1}, {"b":2}]
//	b'{"a":1}{"b":2}'.decode_json_stream()  // return [{"a":1}, {"b":2}]
//
// decode_json_stream_string_numbers returns a list of objects described by the
// JSON stream of the receiver or parameter except that numbers will be
// represented as the literal string syntax used to represent them:
//
//	<bytes>.decode_json_stream_string_numbers() -> <list<dyn>>
//	<string>.decode_json_stream_string_numbers() -> <list<dyn>>
//	decode_json_stream_string_numbers(<bytes>) -> <list<dyn>>
//	decode_json_stream_string_numbers(<string>) -> <list<dyn>>
//
// Examples:
//
//	'{"a":1}{"b":2}'.decode_json_stream_string_numbers()   // return [{"a":"1"}, {"b":"2"}]
//	b'{"a":1}{"b":2}'.decode_json_stream_string_numbers()  // return [{"a":"1"}, {"b":"2"}]
//
// # Decode JSON Stream (Lazy)
//
// decode_json_stream_lazy returns a lazy iterable that decodes concatenated
// JSON values on demand from the receiver. Unlike decode_json_stream, values
// are not materialised into a list; each value is decoded when the iterator
// advances. This is useful for streaming large payloads through a
// comprehension without holding all decoded records in memory.
//
// The receiver may be bytes, string, or a stream value (from stream_gzip or
// stream_zip). When backed by a stream, decompression and decoding happen
// together with no intermediate buffer.
//
//	<bytes>.decode_json_stream_lazy() -> <iterable<dyn>>
//	<string>.decode_json_stream_lazy() -> <iterable<dyn>>
//	<stream>.decode_json_stream_lazy() -> <iterable<dyn>>
//
// decode_json_stream_lazy_string_numbers is the same but uses UseNumber
// decoding for integer precision beyond 2^52.
func JSON(adapter types.Adapter) cel.EnvOption {
	if adapter == nil {
		adapter = types.DefaultTypeAdapter
	}
	return cel.Lib(jsonLib{adapter})
}

type jsonLib struct {
	adapter types.Adapter
}

func (l jsonLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function("encode_json",
			cel.MemberOverload(
				"dyn_encode_json",
				[]*cel.Type{cel.DynType},
				cel.StringType,
				cel.UnaryBinding(catch(encodeJSON)),
			),
			cel.Overload(
				"encode_json_dyn",
				[]*cel.Type{cel.DynType},
				cel.StringType,
				cel.UnaryBinding(catch(encodeJSON)),
			),
		),

		cel.Function("decode_json",
			cel.MemberOverload(
				"string_decode_json",
				[]*cel.Type{cel.StringType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSON)),
			),
			cel.Overload(
				"decode_json_string",
				[]*cel.Type{cel.StringType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSON)),
			),
			cel.MemberOverload(
				"bytes_decode_json",
				[]*cel.Type{cel.BytesType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSON)),
			),
			cel.Overload(
				"decode_json_bytes",
				[]*cel.Type{cel.BytesType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSON)),
			),
		),

		cel.Function("decode_json_string_numbers",
			cel.MemberOverload(
				"string_decode_json_string_numbers",
				[]*cel.Type{cel.StringType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSONUseNumber)),
			),
			cel.Overload(
				"decode_json_string_numbers_string",
				[]*cel.Type{cel.StringType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSONUseNumber)),
			),
			cel.MemberOverload(
				"bytes_decode_json_string_numbers",
				[]*cel.Type{cel.BytesType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSONUseNumber)),
			),
			cel.Overload(
				"decode_json_string_numbers_bytes",
				[]*cel.Type{cel.BytesType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSONUseNumber)),
			),
		),

		cel.Function("decode_json_stream",
			cel.MemberOverload(
				"string_decode_json_stream",
				[]*cel.Type{cel.StringType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSONStream)),
			),
			cel.Overload(
				"decode_json_stream_string",
				[]*cel.Type{cel.StringType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSONStream)),
			),
			cel.MemberOverload(
				"bytes_decode_json_stream",
				[]*cel.Type{cel.BytesType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSONStream)),
			),
			cel.Overload(
				"decode_json_stream_bytes",
				[]*cel.Type{cel.BytesType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSONStream)),
			),
		),

		cel.Function("decode_json_stream_string_numbers",
			cel.MemberOverload(
				"string_decode_json_stream_string_numbers",
				[]*cel.Type{cel.StringType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSONStreamUseNumber)),
			),
			cel.Overload(
				"decode_json_stream_string_numbers_string",
				[]*cel.Type{cel.StringType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSONStreamUseNumber)),
			),
			cel.MemberOverload(
				"bytes_decode_json_stream_string_numbers",
				[]*cel.Type{cel.BytesType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSONStreamUseNumber)),
			),
			cel.Overload(
				"decode_json_stream_string_numbers_bytes",
				[]*cel.Type{cel.BytesType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSONStreamUseNumber)),
			),
		),

		cel.Function("decode_json_stream_lazy",
			cel.MemberOverload(
				"stream_decode_json_stream_lazy",
				[]*cel.Type{streamCELType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSONStreamLazy)),
			),
			cel.MemberOverload(
				"bytes_decode_json_stream_lazy",
				[]*cel.Type{cel.BytesType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSONStreamLazy)),
			),
			cel.MemberOverload(
				"string_decode_json_stream_lazy",
				[]*cel.Type{cel.StringType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSONStreamLazy)),
			),
		),

		cel.Function("decode_json_stream_lazy_string_numbers",
			cel.MemberOverload(
				"stream_decode_json_stream_lazy_string_numbers",
				[]*cel.Type{streamCELType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSONStreamLazyUseNumber)),
			),
			cel.MemberOverload(
				"bytes_decode_json_stream_lazy_string_numbers",
				[]*cel.Type{cel.BytesType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSONStreamLazyUseNumber)),
			),
			cel.MemberOverload(
				"string_decode_json_stream_lazy_string_numbers",
				[]*cel.Type{cel.StringType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeJSONStreamLazyUseNumber)),
			),
		),
	}
}

func (jsonLib) ProgramOptions() []cel.ProgramOption { return nil }

func encodeJSON(val ref.Val) ref.Val {
	var v interface{}
	// Avoid type conversions if possible.
	switch under := val.Value().(type) {
	case map[string]any:
		v = under
	case map[ref.Val]ref.Val, []ref.Val:
		pb, err := val.ConvertToNative(structpbValueType)
		if err != nil {
			return types.NewErr("failed proto conversion: %v", err)
		}
		v = pb.(*structpb.Value).AsInterface()
	default:
		var err error
		typ, ok := encodableTypes[val.Type()]
		if ok {
			v, err = val.ConvertToNative(typ)
			if err != nil {
				// This should never happen.
				panic(fmt.Sprintf("json encode mapping out of sync: %v", err))
			}
		} else {
			for _, typ := range protobufTypes {
				v, err = val.ConvertToNative(typ)
				if err != nil {
					v = nil
				} else {
					break
				}
			}
		}
		if v == nil {
			return types.NewErr("failed to get native value for JSON")
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return types.NewErr("failed to marshal value to JSON: %v", err)
	}
	return types.String(b)
}

func (l jsonLib) decodeJSON(val ref.Val) ref.Val {
	var (
		v   interface{}
		err error
	)
	switch msg := val.(type) {
	case types.Bytes:
		err = json.Unmarshal([]byte(msg), &v)
	case types.String:
		err = json.Unmarshal([]byte(msg), &v)
	default:
		return types.NoSuchOverloadErr()
	}
	if err != nil {
		return types.NewErr("failed to unmarshal JSON message: %v", err)
	}
	return l.adapter.NativeToValue(v)
}

func (l jsonLib) decodeJSONUseNumber(val ref.Val) ref.Val {
	var r io.Reader
	switch msg := val.(type) {
	case types.Bytes:
		r = bytes.NewReader(msg)
	case types.String:
		r = bytes.NewReader([]byte(msg))
	default:
		return types.NoSuchOverloadErr()
	}
	dec := json.NewDecoder(r)
	dec.UseNumber()
	var v any
	err := dec.Decode(&v)
	if err != nil {
		if err == io.EOF {
			err = errors.New("unexpected end of JSON input")
		}
		return types.NewErr("failed to unmarshal JSON message: %v", err)
	}
	tok, err := dec.Token()
	switch err {
	case nil:
		return types.NewErr("failed to unmarshal JSON message: invalid character '%s' after top-level value", tok)
	case io.EOF:
	default:
		var buf bytes.Buffer
		io.Copy(&buf, dec.Buffered())
		b := bytes.TrimSpace(buf.Bytes())
		if len(b) != 0 {
			return types.NewErr("failed to unmarshal JSON message: invalid character '%c' after top-level value", b[0])
		}
	}
	return l.adapter.NativeToValue(v)
}

func (l jsonLib) decodeJSONStream(val ref.Val) ref.Val {
	var r io.Reader
	switch msg := val.(type) {
	case types.Bytes:
		r = bytes.NewReader(msg)
	case types.String:
		r = bytes.NewReader([]byte(msg))
	default:
		return types.NoSuchOverloadErr()
	}
	var s []interface{}
	dec := json.NewDecoder(r)
	for dec.More() {
		var v interface{}
		err := dec.Decode(&v)
		if err != nil {
			return types.NewErr("failed to unmarshal JSON stream: %v", err)
		}
		s = append(s, v)
	}
	return l.adapter.NativeToValue(s)
}

func (l jsonLib) decodeJSONStreamUseNumber(val ref.Val) ref.Val {
	var r io.Reader
	switch msg := val.(type) {
	case types.Bytes:
		r = bytes.NewReader(msg)
	case types.String:
		r = bytes.NewReader([]byte(msg))
	default:
		return types.NoSuchOverloadErr()
	}
	var s []interface{}
	dec := json.NewDecoder(r)
	dec.UseNumber()
	for dec.More() {
		var v interface{}
		err := dec.Decode(&v)
		if err != nil {
			return types.NewErr("failed to unmarshal JSON stream: %v", err)
		}
		s = append(s, v)
	}
	return l.adapter.NativeToValue(s)
}

func (l jsonLib) decodeJSONStreamLazy(val ref.Val) ref.Val {
	return l.lazyStream(val, false)
}

func (l jsonLib) decodeJSONStreamLazyUseNumber(val ref.Val) ref.Val {
	return l.lazyStream(val, true)
}

func (l jsonLib) lazyStream(val ref.Val, useNum bool) ref.Val {
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
	return &lazyJSONStream{reader: r, adapter: l.adapter, useNum: useNum}
}

var (
	_ ref.Val         = (*lazyJSONStream)(nil)
	_ traits.Iterable = (*lazyJSONStream)(nil)
	_ traits.Iterator = (*jsonStreamIterator)(nil)
)

// lazyJSONStreamRefType is the runtime type for lazy JSON stream iterables.
var lazyJSONStreamRefType = types.NewObjectType("lazy_json_stream", traits.IterableType)

// lazyJSONStream is a ref.Val implementing traits.Iterable that decodes
// concatenated JSON values on demand from an io.Reader. Each call to the
// iterator's Next() decodes one value; previously decoded values are not
// retained. This enables streaming decode of large NDJSON or concatenated
// JSON payloads without materialising the full list.
//
// The underlying reader is consumed once. A second Iterator() call returns
// an exhausted iterator.
type lazyJSONStream struct {
	reader  io.Reader
	adapter types.Adapter
	useNum  bool
}

func (s *lazyJSONStream) ConvertToNative(typeDesc reflect.Type) (any, error) {
	return nil, fmt.Errorf("lazy JSON streams cannot be converted to %v", typeDesc)
}

func (s *lazyJSONStream) ConvertToType(typeVal ref.Type) ref.Val {
	if typeVal == types.TypeType {
		return types.NewTypeValue("lazy_json_stream")
	}
	return types.NewErr("type conversion error from 'lazy_json_stream' to '%s'", typeVal.TypeName())
}

func (s *lazyJSONStream) Equal(other ref.Val) ref.Val {
	return types.NewErr("lazy JSON streams are not comparable")
}

func (s *lazyJSONStream) Type() ref.Type { return lazyJSONStreamRefType }

func (s *lazyJSONStream) Value() any { return s.reader }

func (s *lazyJSONStream) Iterator() traits.Iterator {
	dec := json.NewDecoder(s.reader)
	if s.useNum {
		dec.UseNumber()
	}
	return &jsonStreamIterator{dec: dec, adapter: s.adapter}
}

// jsonStreamIterator wraps a json.Decoder as a traits.Iterator.
type jsonStreamIterator struct {
	dec     *json.Decoder
	adapter types.Adapter
}

func (it *jsonStreamIterator) ConvertToNative(typeDesc reflect.Type) (any, error) {
	return nil, fmt.Errorf("JSON stream iterators cannot be converted to %v", typeDesc)
}

func (it *jsonStreamIterator) ConvertToType(typeVal ref.Type) ref.Val {
	return types.NewErr("type conversion error from 'json_stream_iterator' to '%s'", typeVal.TypeName())
}

func (it *jsonStreamIterator) Equal(other ref.Val) ref.Val {
	return types.NewErr("JSON stream iterators are not comparable")
}

func (it *jsonStreamIterator) Type() ref.Type { return types.IteratorType }

func (it *jsonStreamIterator) Value() any { return it.dec }

func (it *jsonStreamIterator) HasNext() ref.Val {
	return types.Bool(it.dec.More())
}

func (it *jsonStreamIterator) Next() ref.Val {
	var v any
	if err := it.dec.Decode(&v); err != nil {
		return types.NewErr("decode: %v", err)
	}
	return it.adapter.NativeToValue(v)
}
