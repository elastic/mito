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
	"fmt"
	"io"

	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
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
	}
}

func (jsonLib) ProgramOptions() []cel.ProgramOption { return nil }

func encodeJSON(val ref.Val) ref.Val {
	var v interface{}
	// Avoid type conversions if possible.
	switch under := val.Value().(type) {
	case map[string]any:
		v = under
	case map[ref.Val]ref.Val:
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
