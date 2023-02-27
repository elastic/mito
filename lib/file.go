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
	"io"
	"os"
	"sort"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter/functions"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// File returns a cel.EnvOption to configure extended functions for reading files.
// It takes a mapping of mimetypes to transforms to allow reading specific mime
// type. The values in the map must be one of: func([]byte), func(io.Reader) io.Reader,
// func(io.Reader) (io.Reader, error) or func(io.Reader) ref.Val. If the
// transform is func([]byte) it is expected to mutate the bytes in place.
//
// # Dir
//
// dir returns either a directory for the provided path:
//
//	dir(<string>) -> <list<map<string,dyn>>>
//
// Examples:
//
//	dir('subdir')
//
//	will return something like:
//
//	[
//	    {
//	        "is_dir": true,
//	        "mod_time": "2022-04-05T20:53:11.923840504+09:30",
//	        "name": "subsubdir",
//	        "size": 4096
//	    },
//	    {
//	        "is_dir": false,
//	        "mod_time": "2022-04-05T20:53:11.923840504+09:30",
//	        "name": "a.txt",
//	        "size": 13
//	    },
//	    {
//	        "is_dir": false,
//	        "mod_time": "2022-04-05T20:53:11.923840504+09:30",
//	        "name": "b.txt",
//	        "size": 11
//	    }
//	]
//
// # File
//
// file returns either a <bytes> or a <dyn> depending on whether it is called
// with one parameter or two:
//
//	file(<string>) -> <bytes>
//	file(<string>, <string>) -> <dyn>
//
// The first parameter is a file path and the second is a look-up into the
// transforms map provided by to the File cel.EnvOption.
//
// Examples:
//
//	Given a file hello.txt:
//	   world!
//
//	And the following transforms map (rot13 is a transforming reader):
//
//	   map[string]interface{}{
//	       "text/rot13": func(r io.Reader) io.Reader { return rot13{r} },
//	       "text/upper": func(p []byte) {
//	           for i, b := range p {
//	               if 'a' <= b && b <= 'z' {
//	                   p[i] &^= 'a' - 'A'
//	               }
//	           }
//	       },
//	   }
//
//	string(file('hello.txt'))                 // return "world!\n"
//	string(file('hello.txt', 'text/rot13'))   // return "jbeyq!\n"
//	string(file('hello.txt', 'text/upper'))   // return "WORLD!\n"
func File(mimetypes map[string]interface{}) cel.EnvOption {
	return cel.Lib(fileLib{transforms: mimetypes})
}

type fileLib struct {
	transforms map[string]interface{}
}

func (fileLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Declarations(
			decls.NewFunction("dir",
				decls.NewOverload(
					"dir_string",
					[]*expr.Type{decls.String},
					decls.NewListType(decls.NewMapType(decls.String, decls.Dyn)),
				),
			),
			decls.NewFunction("file",
				decls.NewOverload(
					"file_string",
					[]*expr.Type{decls.String},
					decls.Bytes,
				),
				decls.NewOverload(
					"file_string_string",
					[]*expr.Type{decls.String, decls.String},
					decls.Dyn,
				),
			),
		),
	}
}

func (l fileLib) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{
		cel.Functions(
			&functions.Overload{
				Operator: "dir_string",
				Unary:    readDir,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "file_string",
				Unary:    readFile,
			},
			&functions.Overload{
				Operator: "file_string_string",
				Binary:   l.readMIMEFile,
			},
		),
	}
}

func readDir(arg ref.Val) ref.Val {
	path, ok := arg.(types.String)
	if !ok {
		return types.ValOrErr(path, "no such overload for dir: %s", arg.Type())
	}
	f, err := os.Open(string(path))
	if err != nil {
		return types.NewErr("dir: %v", err)
	}
	dir, err := f.ReadDir(0)
	if err != nil {
		return types.NewErr("dir: %v", err)
	}
	// Stabilise order across platforms.
	sort.Slice(dir, func(i, j int) bool { return dir[i].Name() < dir[j].Name() })
	res := make([]map[string]interface{}, len(dir))
	for i, e := range dir {
		fi, err := e.Info()
		if err != nil {
			return types.NewErr("dir: %v", err)
		}
		res[i] = map[string]interface{}{
			"name":     e.Name(),
			"is_dir":   e.IsDir(),
			"size":     fi.Size(),
			"mod_time": fi.ModTime(),
		}
	}
	return types.NewDynamicList(types.DefaultTypeAdapter, res)
}

func readFile(arg ref.Val) ref.Val {
	path, ok := arg.(types.String)
	if !ok {
		return types.ValOrErr(path, "no such overload for file: %s", arg.Type())
	}
	b, err := os.ReadFile(string(path))
	if err != nil {
		return types.NewErr("file: %v", err)
	}
	return types.Bytes(b)
}

func (l fileLib) readMIMEFile(arg0, arg1 ref.Val) ref.Val {
	path, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(path, "no such overload for file path: %s", arg0.Type())
	}
	mimetype, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(mimetype, "no such overload for mime type: %s", arg1.Type())
	}
	transform, ok := l.transforms[string(mimetype)]
	if !ok {
		return types.NewErr("unknown transform: %q", mimetype)
	}
	f, err := os.Open(string(path))
	if err != nil {
		return types.NewErr("file: %v", err)
	}
	defer f.Close()
	switch transform := transform.(type) {
	case func([]byte):
		var buf bytes.Buffer
		_, err := io.Copy(&buf, transformReader{
			r: f, transform: transform,
		})
		if err != nil {
			return types.NewErr("file: %v", err)
		}
		return types.Bytes(buf.Bytes())
	case func(io.Reader) io.Reader:
		var buf bytes.Buffer
		_, err := io.Copy(&buf, transform(f))
		if err != nil {
			return types.NewErr("file: %v", err)
		}
		return types.Bytes(buf.Bytes())
	case func(io.Reader) (io.Reader, error):
		var buf bytes.Buffer
		r, err := transform(f)
		if err != nil {
			return types.NewErr("file: %v", err)
		}
		_, err = io.Copy(&buf, r)
		if err != nil {
			return types.NewErr("file: %v", err)
		}
		return types.Bytes(buf.Bytes())
	case func(io.Reader) ref.Val:
		return transform(f)
	}
	return types.NewErr("invalid transform: %T", transform)
}
