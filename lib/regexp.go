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
	"regexp"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter/functions"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// Regexp returns a cel.EnvOption to configure extended functions for
// using regular expressions on strings and bytes. It takes a mapping of
// names to Go regular expressions. The names are used to specify the pattern
// in the CEL regexp call.
//
// Each function corresponds to methods on regexp.Regexp in the Go standard
// library.
//
// For the examples below assume an input patterns map:
//
//     map[string]*regexp.Regexp{
//         "foo":     regexp.MustCompile("foo(.)"),
//         "foo_rep": regexp.MustCompile("(f)oo([ld])"),
//     }
//
// RE Match
//
// Returns whether the named pattern matches the receiver:
//
//     <bytes>.re_match(<string>) -> <bool>
//     <string>.re_match(<string>) -> <bool>
//
// Examples:
//
//     'food'.re_match('foo')    // return true
//     b'food'.re_match(b'foo')  // return true
//
//
// RE Find
//
// Returns a string or bytes of the named pattern's match:
//
//     <bytes>.re_find(<string>) -> <bytes>
//     <string>.re_find(<string>) -> <string>
//
// Examples:
//
//     'food'.re_find('foo')    // return "food"
//     b'food'.re_find(b'foo')  // return "Zm9vZA=="
//
//
// RE Find All
//
// Returns a list of strings or bytes of all the named pattern's matches:
//
//     <bytes>.re_find_all(<string>) -> <list<bytes>>
//     <string>.re_find_all(<string>) -> <list<string>>
//
// Examples:
//
//     'food fool'.re_find_all('foo')  // return ["food", "fool"]
//     b'food fool'.re_find_all(b'foo')  // return ["Zm9vZA==", "Zm9vZA=="]
//
//
// RE Find Submatch
//
// Returns a list of strings or bytes of the named pattern's submatches:
//
//     <bytes>.re_find_submatch(<string>) -> <list<bytes>>
//     <string>.re_find_submatch(<string>) -> <list<string>>
//
// Examples:
//
//     'food fool'.re_find_submatch('foo')   // return ["food", "d"]
//     b'food fool'.re_find_submatch('foo')  // return ["Zm9vZA==", "ZA=="]
//
//
// RE Find All Submatch
//
// Returns a list of lists of strings or bytes of all the named pattern's submatches:
//
//     <bytes>.re_find_all_submatch(<string>) -> <list<list<bytes>>>
//     <string>.re_find_all_submatch(<string>) -> <list<list<string>>>
//
// Examples:
//
//     'food fool'.re_find_all_submatch('foo')  // return [["food", "d"], ["fool", "l"]]
//
//
// RE Replace All
//
// Returns a strings or bytes applying a replacement to all matches of the named
// pattern:
//
//     <bytes>.re_replace_all(<string>, <bytes>) -> <bytes>
//     <string>.re_replace_all(<string>, <string>) -> <string>
//
// Examples:
//
//     'food fool'.re_replace_all('foo_rep', '${1}u${2}')    // return "fud ful"
//     b'food fool'.re_replace_all('foo_rep', b'${1}u${2}')  // return "ZnVkIGZ1bA=="
//
func Regexp(patterns map[string]*regexp.Regexp) cel.EnvOption {
	return cel.Lib(regexpLib(patterns))
}

type regexpLib map[string]*regexp.Regexp

func (l regexpLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Declarations(
			decls.NewFunction("re_match",
				decls.NewInstanceOverload(
					"typeV_re_match_string",
					[]*expr.Type{decls.Dyn, decls.String},
					decls.Bool,
				),
			),
			decls.NewFunction("re_find",
				decls.NewParameterizedInstanceOverload(
					"typeV_re_find_string",
					[]*expr.Type{typeV, decls.String},
					typeV,
					[]string{"V"},
				),
			),
			decls.NewFunction("re_find_all",
				decls.NewParameterizedInstanceOverload(
					"typeV_re_find_all_string",
					[]*expr.Type{typeV, decls.String},
					decls.NewListType(typeV),
					[]string{"V"},
				),
			),
			decls.NewFunction("re_find_submatch",
				decls.NewParameterizedInstanceOverload(
					"typeV_re_find_submatch_string",
					[]*expr.Type{typeV, decls.String},
					decls.NewListType(typeV),
					[]string{"V"},
				),
			),
			decls.NewFunction("re_find_all_submatch",
				decls.NewParameterizedInstanceOverload(
					"typeV_re_find_all_submatch_string",
					[]*expr.Type{typeV, decls.String},
					decls.NewListType(decls.NewListType(typeV)),
					[]string{"V"},
				),
			),
			decls.NewFunction("re_replace_all",
				decls.NewParameterizedInstanceOverload(
					"typeV_re_replace_all_string_dyn",
					[]*expr.Type{typeV, decls.String, typeV},
					typeV,
					[]string{"V"},
				),
			),
		),
	}
}

func (l regexpLib) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{
		cel.Functions(
			&functions.Overload{
				Operator: "typeV_re_match_string",
				Binary:   l.match,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "typeV_re_find_string",
				Binary:   l.find,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "typeV_re_find_all_string",
				Binary:   l.findAll,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "typeV_re_find_submatch_string",
				Binary:   l.findSubmatch,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "typeV_re_find_all_submatch_string",
				Binary:   l.findAllSubmatch,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "typeV_re_replace_all_string_dyn",
				Function: l.replaceAll,
			},
		),
	}
}

func (l regexpLib) match(arg1, arg2 ref.Val) ref.Val {
	patName, ok := arg2.(types.String)
	if !ok {
		return types.ValOrErr(patName, "no such overload")
	}
	re, ok := l[string(patName)]
	if !ok {
		return types.NewErr("no regexp %s", patName)
	}
	switch src := arg1.(type) {
	case types.Bytes:
		return types.Bool(re.Match(src))
	case types.String:
		return types.Bool(re.MatchString(string(src)))
	default:
		return types.NewErr("invalid type for match: %s", arg1.Type())
	}
}

func (l regexpLib) find(arg1, arg2 ref.Val) ref.Val {
	patName, ok := arg2.(types.String)
	if !ok {
		return types.ValOrErr(patName, "no such overload")
	}
	re, ok := l[string(patName)]
	if !ok {
		return types.NewErr("no regexp %s", patName)
	}
	switch src := arg1.(type) {
	case types.Bytes:
		return types.Bytes(re.Find(src))
	case types.String:
		return types.String(re.FindString(string(src)))
	default:
		return types.NewErr("invalid type for find: %s", arg1.Type())
	}
}

func (l regexpLib) findAll(arg1, arg2 ref.Val) ref.Val {
	patName, ok := arg2.(types.String)
	if !ok {
		return types.ValOrErr(patName, "no such overload")
	}
	re, ok := l[string(patName)]
	if !ok {
		return types.NewErr("no regexp %s", patName)
	}
	switch src := arg1.(type) {
	case types.Bytes:
		return types.DefaultTypeAdapter.NativeToValue(re.FindAll(src, -1))
	case types.String:
		return types.DefaultTypeAdapter.NativeToValue(re.FindAllString(string(src), -1))
	default:
		return types.NewErr("invalid type for find_all: %s", arg1.Type())
	}
}

func (l regexpLib) findSubmatch(arg1, arg2 ref.Val) ref.Val {
	patName, ok := arg2.(types.String)
	if !ok {
		return types.ValOrErr(patName, "no such overload")
	}
	re, ok := l[string(patName)]
	if !ok {
		return types.NewErr("no regexp %s", patName)
	}
	switch src := arg1.(type) {
	case types.Bytes:
		return types.DefaultTypeAdapter.NativeToValue(re.FindSubmatch(src))
	case types.String:
		return types.DefaultTypeAdapter.NativeToValue(re.FindStringSubmatch(string(src)))
	default:
		return types.NewErr("invalid type for find_submatch: %s", arg1.Type())
	}
}

func (l regexpLib) findAllSubmatch(arg1, arg2 ref.Val) ref.Val {
	patName, ok := arg2.(types.String)
	if !ok {
		return types.ValOrErr(patName, "no such overload")
	}
	re, ok := l[string(patName)]
	if !ok {
		return types.NewErr("no regexp %s", patName)
	}
	switch src := arg1.(type) {
	case types.Bytes:
		return types.DefaultTypeAdapter.NativeToValue(re.FindAllSubmatch(src, -1))
	case types.String:
		return types.DefaultTypeAdapter.NativeToValue(re.FindAllStringSubmatch(string(src), -1))
	default:
		return types.NewErr("invalid type for find_all_submatch: %s", arg1.Type())
	}
}

func (l regexpLib) replaceAll(args ...ref.Val) ref.Val {
	if len(args) != 3 {
		return types.NoSuchOverloadErr()
	}
	patName, ok := args[1].(types.String)
	if !ok {
		return types.ValOrErr(patName, "no such overload")
	}
	re, ok := l[string(patName)]
	if !ok {
		return types.NewErr("no regexp %s", patName)
	}
	switch src := args[0].(type) {
	case types.Bytes:
		repl, ok := args[2].(types.Bytes)
		if !ok {
			return types.ValOrErr(repl, "no such overload")
		}
		return types.Bytes(re.ReplaceAll(src, repl))
	case types.String:
		repl, ok := args[2].(types.String)
		if !ok {
			return types.ValOrErr(repl, "no such overload")
		}
		return types.String(re.ReplaceAllString(string(src), string(repl)))
	default:
		return types.NewErr("invalid type for replace_all: %s", args[0].Type())
	}
}
