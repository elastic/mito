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
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"io"
	"os"

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

// CSVHeader provides a file transform that returns a <list<map<string,string>>> from an
// io.Reader holding text/csv data. It should be handed to the File or MIME
// lib with
//
//  File(map[string]interface{}{
//  	"text/csv; header=present": lib.CSVHeader,
//  })
//
// or
//
//  MIME(map[string]interface{}{
//  	"text/csv; header=present": lib.CSVHeader,
//  })
//
// It will then be able to be used in a file or mime call.
//
// Example:
//
//     Given a file hello.csv:
//        "first","second","third"
//        1,2,3
//
//     file('hello.csv', 'text/csv; header=present')
//
//     will return:
//
//     [{"first": "1", "second": "2", "third": "3"}]
//
func CSVHeader(r io.Reader) ref.Val {
	var vals []map[string]string
	cr := csv.NewReader(r)
	var h []string
	for i := 0; ; i++ {
		rec, err := cr.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return types.NewErr("csv: %v", err)
		}
		if i == 0 {
			h = rec
			continue
		}
		v := make(map[string]string, len(h))
		for j, n := range h {
			v[n] = rec[j]
		}
		vals = append(vals, v)
	}
	return types.NewDynamicList(types.DefaultTypeAdapter, vals)
}

// CSVNoHeader provides a file transform that returns a <list<list<string>>> from an
// io.Reader holding text/csv data. It should be handed to the File or MIME
// lib with
//
//  File(map[string]interface{}{
//  	"text/csv; header=absent": lib.CSVNoHeader,
//  })
//
// or
//
//  MIME(map[string]interface{}{
//  	"text/csv; header=absent": lib.CSVNoHeader,
//  })
//
// It will then be able to be used in a file or mime call.
//
// Example:
//
//     Given a file hello.csv:
//        "first","second","third"
//        1,2,3
//
//     file('hello.csv', 'text/csv; header=absent')
//
//     will return:
//
//     [["first", "second", "third"], ["1", "2", "3"]]
//
func CSVNoHeader(r io.Reader) ref.Val {
	vals, err := csv.NewReader(r).ReadAll()
	if err != nil {
		return types.NewErr("csv: %v", err)
	}
	return types.NewDynamicList(types.DefaultTypeAdapter, vals)
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

// Zip provides a file transform that returns a <map<dyn>> from an io.Reader
// holding a zip archive data. It should be handed to the File or MIME lib with
//
//  File(map[string]interface{}{
//  	"application/zip": lib.Zip,
//  })
//
// or
//
//  MIME(map[string]interface{}{
//  	"application/zip": lib.Zip,
//  })
//
// It will then be able to be used in a file or mime call.
//
// The returned map reflects the structure of the Go zip.Reader struct.
//
// Example:
//
//     file('hello.zip', 'application/zip')
//
//     might return:
//
//     {
//         "Comment": "hello zip file"
//         "File": [
//             {
//                 "CRC32": 0,
//                 "Comment": "",
//                 "Data": "",
//                 "Extra": "VVQFAAMCCFhidXgLAAEE6AMAAAToAwAA",
//                 "IsDir": true,
//                 "Modified": "2022-04-14T21:09:46+09:30",
//                 "Name": "subdir/",
//                 "NonUTF8": false,
//                 "Size": 0
//             },
//             {
//                 "CRC32": 30912436,
//                 "Comment": "",
//                 "Data": "aGVsbG8gd29ybGQhCg==",
//                 "Extra": "VVQFAAP0B1hidXgLAAEE6AMAAAToAwAA",
//                 "IsDir": false,
//                 "Modified": "2022-04-14T21:09:32+09:30",
//                 "Name": "subdir/a.txt",
//                 "NonUTF8": false,
//                 "Size": 13
//             }
//         ]
//     }
//
// Note that the entire contents of the zip file is expanded into memory.
func Zip(r io.Reader) ref.Val {
	var z *zip.Reader
	switch r := r.(type) {
	case *os.File:
		fi, err := r.Stat()
		if err != nil {
			return types.NewErr("zip: %s", err)
		}
		z, err = zip.NewReader(r, fi.Size())
		if err != nil {
			return types.NewErr("zip: %s", err)
		}
	default:
		var buf bytes.Buffer
		_, err := io.Copy(&buf, r)
		if err != nil {
			return types.NewErr("zip: %s", err)
		}
		br := bytes.NewReader(buf.Bytes())
		z, err = zip.NewReader(br, br.Size())
		if err != nil {
			return types.NewErr("zip: %s", err)
		}
	}
	return expandZip(z)
}

func expandZip(z *zip.Reader) ref.Val {
	var files []map[string]interface{}
	for _, f := range z.File {
		rc, err := f.Open()
		if err != nil {
			return types.NewErr("zip: %s", err)
		}
		var buf bytes.Buffer
		_, err = io.Copy(&buf, rc)
		if err != nil {
			return types.NewErr("zip: %s", err)
		}
		err = rc.Close()
		if err != nil {
			return types.NewErr("zip: %s", err)
		}
		fh := f.FileHeader
		fi := fh.FileInfo()
		files = append(files, map[string]interface{}{
			"Name":     fh.Name,
			"Comment":  fh.Comment,
			"IsDir":    fi.IsDir(),
			"Size":     fi.Size(),
			"NonUTF8":  fh.NonUTF8,
			"Modified": fh.Modified,
			"CRC32":    fh.CRC32,
			"Extra":    fh.Extra,
			"Data":     buf.Bytes(),
		})
	}
	return types.DefaultTypeAdapter.NativeToValue(map[string]interface{}{
		"File":    files,
		"Comment": z.Comment,
	})
}
