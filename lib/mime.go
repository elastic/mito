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
	"encoding/json"
	"io"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter/functions"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// MIME returns a cel.EnvOption to configure extended functions for reading files.
// It takes a mapping of mimetypes to transforms to allow interpreting specific mime
// type. The values in the map must be one of: func([]byte), func(io.Reader) io.Reader,
// func(io.Reader) (io.Reader, error) or func(io.Reader) ref.Val. If the
// transform is func([]byte) it is expected to mutate the bytes in place.
//
// MIME
//
// mime returns <dyn> interpreted through the registered MIME type:
//
//     <bytes>.mime(<string>) -> <dyn>
//
// Examples:
//
//     string(b"hello world!".mime("text/rot13"))  // return "uryyb jbeyq!"
//     string(b"hello world!".mime("text/upper"))  // return "HELLO WORLD!"
//     string(b"\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\xcaH\xcd\xc9\xc9W(\xcf/\xcaIQ\x04\x04\x00\x00\xff\xffm´\x03\f\x00\x00\x00"
//         .mime("application/gzip"))  // return "hello world!"
//
//
// See also File and NDJSON.
//
func MIME(mimetypes map[string]interface{}) cel.EnvOption {
	return cel.Lib(mimeLib{transforms: mimetypes})
}

type mimeLib struct {
	transforms map[string]interface{}
}

func (mimeLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Declarations(
			decls.NewFunction("mime",
				decls.NewInstanceOverload(
					"bytes_mime_string",
					[]*expr.Type{decls.Bytes, decls.String},
					decls.Dyn,
				),
			),
		),
	}
}

func (l mimeLib) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{
		cel.Functions(
			&functions.Overload{
				Operator: "bytes_mime_string",
				Binary:   l.transformMIME,
			},
		),
	}
}

func (l mimeLib) transformMIME(arg0, arg1 ref.Val) ref.Val {
	input, ok := arg0.(types.Bytes)
	if !ok {
		return types.ValOrErr(input, "no such overload for file path: %s", arg0.Type())
	}
	mimetype, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(mimetype, "no such overload for mime type: %s", arg1.Type())
	}
	transform, ok := l.transforms[string(mimetype)]
	if !ok {
		return types.NewErr("unknown transform: %q", mimetype)
	}

	switch transform := transform.(type) {
	case func([]byte):
		c := make([]byte, len(input))
		copy(c, input)
		transform(c)
		return types.Bytes(c)
	case func(io.Reader) io.Reader:
		var buf bytes.Buffer
		_, err := io.Copy(&buf, transform(bytes.NewReader(input)))
		if err != nil {
			return types.NewErr("file: %v", err)
		}
		return types.Bytes(buf.Bytes())
	case func(io.Reader) (io.Reader, error):
		var buf bytes.Buffer
		r, err := transform(bytes.NewReader(input))
		if err != nil {
			return types.NewErr("file: %v", err)
		}
		_, err = io.Copy(&buf, r)
		if err != nil {
			return types.NewErr("file: %v", err)
		}
		return types.Bytes(buf.Bytes())
	case func(io.Reader) ref.Val:
		return transform(bytes.NewReader(input))
	}
	return types.NewErr("invalid transform: %T", transform)
}

type transformReader struct {
	r         io.Reader
	transform func([]byte)
}

func (t transformReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	t.transform(p[:n])
	return n, err
}

// NDJSON provides a file transform that returns a <list<dyn>> from an
// io.Reader holding ND-JSON data. It should be handed to the File or MIME
// lib with
//
//  File(map[string]interface{}{
//  	"application/x-ndjson": lib.NDJSON,
//  })
//
// or
//
//  MIME(map[string]interface{}{
//  	"application/x-ndjson": lib.NDJSON,
//  })
//
// It will then be able to be used in a file or mime call.
//
// Example:
//
//     Given a file hello.ndjson:
//        {"message":"hello"}
//        {"message":"world"}
//
//     file('hello.ndjson', 'application/x-ndjson')
//
//     will return:
//
//     [
//         {
//             "message": "hello"
//         },
//         {
//             "message": "world"
//         }
//     ]
//
// Messages in the ND-JSON stream that are invalid will be added to the list
// as CEL errors and will need to be processed using the try function.
//
// Example:
//
//     Given a file hello.ndjson:
//        {"message":"hello"}
//        {"message":"oops"
//        {"message":"world"}
//
//     file('hello.ndjson', 'application/x-ndjson').map(e, try(e, "error.message"))
//
//     will return:
//
//     [
//         {
//             "message": "hello"
//         },
//         {
//             "error.message": "unexpected end of JSON input: {\"message\":\"oops\""
//         },
//         {
//             "message": "world"
//         }
//     ]
//
func NDJSON(r io.Reader) ref.Val {
	// This is not real ndjson since it doesn't have the
	// stupid requirement for newline line termination.
	var vals []interface{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) == 0 {
			continue
		}
		var v interface{}
		err := json.Unmarshal(sc.Bytes(), &v)
		if err != nil {
			vals = append(vals, types.NewErr("%v: %s", err, sc.Bytes()))
			continue
		}
		vals = append(vals, v)
	}
	err := sc.Err()
	if err != nil {
		return types.NewErr("ndjson: %v", err)
	}
	return types.NewDynamicList(types.DefaultTypeAdapter, vals)
}
