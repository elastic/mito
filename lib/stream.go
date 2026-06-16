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
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"reflect"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// Stream returns a cel.EnvOption to configure stream producer functions.
// Stream producers wrap decompression readers around in-memory bytes,
// returning an opaque streamVal that can be passed to lazy decode functions
// (decode_json_stream_lazy, etc.) for streaming decompression and decoding.
//
// # stream_gzip
//
// stream_gzip returns a stream wrapping a gzip reader over the receiver bytes.
// The compressed bytes remain in memory; decompression happens on demand
// through a ~32 KB internal buffer.
//
//	<bytes>.stream_gzip() -> <stream>
//
// # stream_zip
//
// stream_zip returns a stream wrapping the decompression reader for the
// specified entry (by index) in a zip archive held in the receiver bytes.
// The zip archive must fit in memory since archive/zip requires random access.
//
//	<bytes>.stream_zip(<int>) -> <stream>
func Stream() cel.EnvOption {
	return cel.Lib(streamLib{})
}

type streamLib struct{}

func (streamLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function("stream_gzip",
			cel.MemberOverload(
				"bytes_stream_gzip",
				[]*cel.Type{cel.BytesType},
				streamCELType,
				cel.UnaryBinding(catch(streamGzip)),
			),
		),

		cel.Function("stream_zip",
			cel.MemberOverload(
				"bytes_stream_zip_int",
				[]*cel.Type{cel.BytesType, cel.IntType},
				streamCELType,
				cel.BinaryBinding(catch(streamZip)),
			),
		),
	}
}

func (streamLib) ProgramOptions() []cel.ProgramOption { return nil }

// streamCELType is the compile-time CEL type for stream values.
var streamCELType = cel.ObjectType("stream")

// streamRefType is the runtime ref.Type for stream values.
var streamRefType = types.NewObjectType("stream")

var _ ref.Val = (*streamVal)(nil)

// streamVal is an opaque ref.Val wrapping an io.Reader. It advertises no
// traits: it is not iterable, indexable, sizable, or comparable. The only
// useful thing to do with a stream is pass it to a consuming function such
// as decode_json_stream_lazy.
type streamVal struct {
	reader io.Reader
}

func (s *streamVal) ConvertToNative(typeDesc reflect.Type) (any, error) {
	return nil, fmt.Errorf("stream values cannot be converted to %v", typeDesc)
}

func (s *streamVal) ConvertToType(typeVal ref.Type) ref.Val {
	if typeVal == types.TypeType {
		return types.NewTypeValue("stream")
	}
	return types.NewErr("type conversion error from 'stream' to '%s'", typeVal.TypeName())
}

func (s *streamVal) Equal(other ref.Val) ref.Val {
	return types.NewErr("streams are not comparable")
}

func (s *streamVal) Type() ref.Type { return streamRefType }

func (s *streamVal) Value() any { return s.reader }

func streamGzip(val ref.Val) ref.Val {
	b, ok := val.(types.Bytes)
	if !ok {
		return types.NoSuchOverloadErr()
	}
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return types.NewErr("stream_gzip: %v", err)
	}
	return &streamVal{reader: r}
}

func streamZip(data, index ref.Val) ref.Val {
	b, ok := data.(types.Bytes)
	if !ok {
		return types.NoSuchOverloadErr()
	}
	idx, ok := index.(types.Int)
	if !ok {
		return types.NoSuchOverloadErr()
	}
	br := bytes.NewReader(b)
	zr, err := zip.NewReader(br, br.Size())
	if err != nil {
		return types.NewErr("stream_zip: %v", err)
	}
	i := int(idx)
	if i < 0 || i >= len(zr.File) {
		return types.NewErr("stream_zip: index %d out of range [0, %d)", i, len(zr.File))
	}
	rc, err := zr.File[i].Open()
	if err != nil {
		return types.NewErr("stream_zip: %v", err)
	}
	return &streamVal{reader: rc}
}
