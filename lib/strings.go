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
	"strings"
	"unicode/utf8"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter/functions"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// Strings returns a cel.EnvOption to configure extended functions for
// handling strings.
//
// All functions provided by Strings are methods on the string type or list<string> type
// with the exception of to_valid_utf8 and valid_utf8 which are methods on the bytes type.
//
// Relevant documentation for the methods can obtained from the Go standard library. In all
// cases the first parameter in the Go function corresponds to the CEL method receiver.
//
// String Methods
//
// - compare: strings.Compare(a, b string) int
// - contains_substr: strings.Contains(s, substr string) bool
// - contained_any: strings.ContainsAny(s, chars string) bool
// - count: strings.Count(s, substr string) int
// - equal_fold: strings.EqualFold(s, t string) bool
// - fields: strings.Fields(s string) []string
// - has_prefix: strings.HasPrefix(s, prefix string) bool
// - has_suffix: strings.HasSuffix(s, suffix string) bool
// - index: strings.Index(s, substr string) int
// - index_any: strings.IndexAny(s, chars string) int
// - last_index: strings.LastIndex(s, substr string) int
// - last_index_any: strings.LastIndexAny(s, chars string) int
// - repeat: strings.Repeat(s string, count int) string
// - replace: strings.Replace(s, old, new string, n int) string
// - replace_all: strings.ReplaceAll(s, old, new string) string
// - split: strings.Split(s, sep string) []string
// - split_after: strings.SplitAfter(s, sep string) []string
// - split_after_n: strings.SplitAfterN(s, sep string, n int) []string
// - split_n: strings.SplitN(s, sep string, n int) []string
// - to_lower: strings.ToLower(s string) string
// - to_title: strings.ToTitle(s string) string
// - to_upper: strings.ToUpper(s string) string
// - trim: strings.Trim(s, cutset string) string
// - trim_left: strings.TrimLeft(s, cutset string) string
// - trim_prefix: strings.TrimPrefix(s, prefix string) string
// - trim_right: strings.TrimRight(s, cutset string) string
// - trim_space: strings.TrimSpace(s string) string
// - trim_suffix: strings.TrimSuffix(s, suffix string) string
//
// In addition to the strings package functions, a sub-string method is provided that allows
// string slicing at unicode code point boundaries. It differs from Go's string slicing
// operator which may slice within a code point resulting in invalid UTF-8. The substring
// method will always return a valid UTF-8 string, or an error if the indexes are out of
// bounds or invalid.
//
// - substring: s[start:end]
//
// String List Methods
//
// - join: strings.Join(elems []string, sep string) string
//
// Bytes Methods
//
// The to_valid_utf8 method is equivalent to strings.ToValidUTF8 with the receiver first
// converted to a Go string. This special case is required as CEL does not permit invalid
// UTF-8 string conversions.
//
// - to_valid_utf8: strings.ToValidUTF8(s, replacement string) string
// - valid_utf8: utf8.Valid(s []byte) bool
func Strings() cel.EnvOption {
	return cel.Lib(stringLib{})
}

type stringLib struct{}

func (stringLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Declarations(
			decls.NewFunction("compare",
				decls.NewInstanceOverload(
					"string_compare_string_int",
					[]*expr.Type{decls.String, decls.String},
					decls.Int,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("contains_substr", // required to disambiguate from regexp.contains.
				decls.NewInstanceOverload(
					"string_contains_substr_string_bool",
					[]*expr.Type{decls.String, decls.String},
					decls.Bool,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("contains_any",
				decls.NewInstanceOverload(
					"string_contains_any_string_bool",
					[]*expr.Type{decls.String, decls.String},
					decls.Bool,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("count",
				decls.NewInstanceOverload(
					"string_count_string_int",
					[]*expr.Type{decls.String, decls.String},
					decls.Int,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("equal_fold",
				decls.NewInstanceOverload(
					"string_equal_fold_string_bool",
					[]*expr.Type{decls.String, decls.String},
					decls.Bool,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("fields",
				decls.NewInstanceOverload(
					"string_fields_list_string",
					[]*expr.Type{decls.String},
					listString,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("has_prefix",
				decls.NewInstanceOverload(
					"string_has_prefix_string_bool",
					[]*expr.Type{decls.String, decls.String},
					decls.Bool,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("has_suffix",
				decls.NewInstanceOverload(
					"string_has_suffix_string_bool",
					[]*expr.Type{decls.String, decls.String},
					decls.Bool,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("index",
				decls.NewInstanceOverload(
					"string_index_string_int",
					[]*expr.Type{decls.String, decls.String},
					decls.Int,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("index_any",
				decls.NewInstanceOverload(
					"string_index_any_string_int",
					[]*expr.Type{decls.String, decls.String},
					decls.Int,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("join",
				decls.NewInstanceOverload(
					"list_string_join_string_string",
					[]*expr.Type{listString, decls.String},
					decls.String,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("last_index",
				decls.NewInstanceOverload(
					"string_last_index_string_int",
					[]*expr.Type{decls.String, decls.String},
					decls.Int,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("last_index_any",
				decls.NewInstanceOverload(
					"string_last_index_any_string_int",
					[]*expr.Type{decls.String, decls.String},
					decls.Int,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("repeat",
				decls.NewInstanceOverload(
					"string_repeat_int_string",
					[]*expr.Type{decls.String, decls.Int},
					decls.String,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("replace",
				decls.NewInstanceOverload(
					"string_replace_string_string_int_string",
					[]*expr.Type{decls.String, decls.String, decls.String, decls.Int},
					decls.String,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("replace_all",
				decls.NewInstanceOverload(
					"string_replace_all_string_string_string",
					[]*expr.Type{decls.String, decls.String, decls.String},
					decls.String,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("split",
				decls.NewInstanceOverload(
					"string_split_string_list_string",
					[]*expr.Type{decls.String, decls.String},
					listString,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("split_after",
				decls.NewInstanceOverload(
					"string_split_after_string_list_string",
					[]*expr.Type{decls.String, decls.String},
					listString,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("split_after_n",
				decls.NewInstanceOverload(
					"string_split_after_n_string_int_list_string",
					[]*expr.Type{decls.String, decls.String, decls.Int},
					listString,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("split_n",
				decls.NewInstanceOverload(
					"string_split_n_string_int_list_string",
					[]*expr.Type{decls.String, decls.String, decls.Int},
					listString,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("substring",
				decls.NewInstanceOverload(
					"string_substring_int_int_string",
					[]*expr.Type{decls.String, decls.Int, decls.Int},
					decls.String,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("to_lower",
				decls.NewInstanceOverload(
					"string_to_lower_string",
					[]*expr.Type{decls.String},
					decls.String,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("to_title",
				decls.NewInstanceOverload(
					"string_to_title_string",
					[]*expr.Type{decls.String},
					decls.String,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("to_upper",
				decls.NewInstanceOverload(
					"string_to_upper_string",
					[]*expr.Type{decls.String},
					decls.String,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("to_valid_utf8",
				decls.NewInstanceOverload(
					"bytes_to_valid_utf8_string_string",
					[]*expr.Type{decls.Bytes, decls.String},
					decls.String,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("trim",
				decls.NewInstanceOverload(
					"string_trim_string_string",
					[]*expr.Type{decls.String, decls.String},
					decls.String,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("trim_left",
				decls.NewInstanceOverload(
					"string_trim_left_string_string",
					[]*expr.Type{decls.String, decls.String},
					decls.String,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("trim_prefix",
				decls.NewInstanceOverload(
					"string_trim_prefix_string_string",
					[]*expr.Type{decls.String, decls.String},
					decls.String,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("trim_right",
				decls.NewInstanceOverload(
					"string_trim_right_string_string",
					[]*expr.Type{decls.String, decls.String},
					decls.String,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("trim_space",
				decls.NewInstanceOverload(
					"string_trim_space_string",
					[]*expr.Type{decls.String},
					decls.String,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("trim_suffix",
				decls.NewInstanceOverload(
					"string_trim_suffix_string_string",
					[]*expr.Type{decls.String, decls.String},
					decls.String,
				),
			),
		),
		cel.Declarations(
			decls.NewFunction("valid_utf8",
				decls.NewInstanceOverload(
					"bytes_valid_utf8_bool",
					[]*expr.Type{decls.Bytes},
					decls.Bool,
				),
			),
		),
	}
}

func (l stringLib) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{
		cel.Functions(
			&functions.Overload{
				Operator: "string_compare_string_int",
				Binary:   l.compare,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_contains_substr_string_bool",
				Binary:   l.contains,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_contains_any_string_bool",
				Binary:   l.containsAny,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_count_string_int",
				Binary:   l.count,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_equal_fold_string_bool",
				Binary:   l.equalFold,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_fields_list_string",
				Unary:    l.fields,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_has_prefix_string_bool",
				Binary:   l.hasPrefix,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_has_suffix_string_bool",
				Binary:   l.hasSuffix,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_index_string_int",
				Binary:   l.index,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_index_any_string_int",
				Binary:   l.indexAny,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "list_string_join_string_string",
				Binary:   l.join,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_last_index_string_int",
				Binary:   l.lastIndex,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_last_index_any_string_int",
				Binary:   l.lastIndexAny,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_repeat_int_string",
				Binary:   l.repeat,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_replace_string_string_int_string",
				Function: l.replace,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_replace_all_string_string_string",
				Function: l.replaceAll,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_split_string_list_string",
				Binary:   l.split,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_split_after_string_list_string",
				Binary:   l.splitAfter,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_split_after_n_string_int_list_string",
				Function: l.splitAfterN,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_split_n_string_int_list_string",
				Function: l.splitN,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_substring_int_int_string",
				Function: l.substring,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_to_lower_string",
				Unary:    l.toLower,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_to_title_string",
				Unary:    l.toTitle,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_to_upper_string",
				Unary:    l.toUpper,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "bytes_to_valid_utf8_string_string",
				Binary:   l.toValidUTF8,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_trim_string_string",
				Binary:   l.trim,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_trim_left_string_string",
				Binary:   l.trimLeft,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_trim_prefix_string_string",
				Binary:   l.trimPrefix,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_trim_right_string_string",
				Binary:   l.trimRight,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_trim_space_string",
				Unary:    l.trimSpace,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_trim_suffix_string_string",
				Binary:   l.trimSuffix,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "bytes_valid_utf8_bool",
				Unary:    l.validString,
			},
		),
	}
}

func (l stringLib) compare(arg0, arg1 ref.Val) ref.Val {
	a, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(a, "no such overload for compare")
	}
	b, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(b, "no such overload for compare")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.Compare(string(a), string(b)))
}

func (l stringLib) contains(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for contains_substr")
	}
	substr, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(substr, "no such overload for contains_substr")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.Contains(string(s), string(substr)))
}

func (l stringLib) containsAny(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for contains_any")
	}
	substr, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(substr, "no such overload for contains_any")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.ContainsAny(string(s), string(substr)))
}

func (l stringLib) count(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for count")
	}
	substr, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(substr, "no such overload for count")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.Count(string(s), string(substr)))
}

func (l stringLib) equalFold(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for equal_fold")
	}
	substr, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(substr, "no such overload for equal_fold")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.EqualFold(string(s), string(substr)))
}

func (l stringLib) fields(arg ref.Val) ref.Val {
	s, ok := arg.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for fields")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.Fields(string(s)))
}

func (l stringLib) hasPrefix(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for has_prefix")
	}
	prefix, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(prefix, "no such overload for has_prefix")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.HasPrefix(string(s), string(prefix)))
}

func (l stringLib) hasSuffix(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for has_suffix")
	}
	suffix, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(suffix, "no such overload for has_suffix")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.HasSuffix(string(s), string(suffix)))
}

func (l stringLib) index(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for index")
	}
	substr, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(substr, "no such overload for index")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.Index(string(s), string(substr)))
}

func (l stringLib) indexAny(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for index_any")
	}
	chars, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(chars, "no such overload for index_any")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.IndexAny(string(s), string(chars)))
}

func (l stringLib) join(arg0, arg1 ref.Val) ref.Val {
	elems, err := arg0.ConvertToNative(reflectStringSliceType)
	if err != nil {
		return types.NewErr("no such overload for index_any")
	}
	sep, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(sep, "no such overload for index_any")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.Join(elems.([]string), string(sep)))
}

func (l stringLib) lastIndex(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for last_index")
	}
	substr, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(substr, "no such overload for last_index")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.LastIndex(string(s), string(substr)))
}

func (l stringLib) lastIndexAny(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for last_index_any")
	}
	chars, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(chars, "no such overload for last_index_any")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.LastIndexAny(string(s), string(chars)))
}

func (l stringLib) repeat(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for repeat")
	}
	chars, ok := arg1.(types.Int)
	if !ok {
		return types.ValOrErr(chars, "no such overload for repeat")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.Repeat(string(s), int(chars)))
}

func (l stringLib) replace(args ...ref.Val) ref.Val {
	if len(args) != 4 {
		return types.NewErr("no such overload for replace")
	}
	s, ok := args[0].(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for replace")
	}
	old, ok := args[1].(types.String)
	if !ok {
		return types.ValOrErr(old, "no such overload for replace")
	}
	new, ok := args[2].(types.String)
	if !ok {
		return types.ValOrErr(old, "no such overload for replace")
	}
	n, ok := args[3].(types.Int)
	if !ok {
		return types.ValOrErr(n, "no such overload for replace")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.Replace(string(s), string(old), string(new), int(n)))
}

func (l stringLib) replaceAll(args ...ref.Val) ref.Val {
	if len(args) != 3 {
		return types.NewErr("no such overload for replace_all")
	}
	s, ok := args[0].(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for replace_all")
	}
	old, ok := args[1].(types.String)
	if !ok {
		return types.ValOrErr(old, "no such overload for replace_all")
	}
	new, ok := args[2].(types.String)
	if !ok {
		return types.ValOrErr(new, "no such overload for replace_all")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.ReplaceAll(string(s), string(old), string(new)))
}

func (l stringLib) split(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for split")
	}
	sep, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(sep, "no such overload for split")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.Split(string(s), string(sep)))
}

func (l stringLib) splitAfter(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for split_after")
	}
	sep, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(sep, "no such overload for split_after")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.SplitAfter(string(s), string(sep)))
}

func (l stringLib) splitAfterN(args ...ref.Val) ref.Val {
	if len(args) != 3 {
		return types.NewErr("no such overload for split_after_n")
	}
	s, ok := args[0].(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for split_after_n")
	}
	sep, ok := args[1].(types.String)
	if !ok {
		return types.ValOrErr(sep, "no such overload for split_after_n")
	}
	n, ok := args[2].(types.Int)
	if !ok {
		return types.ValOrErr(n, "no such overload for split_after_n")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.SplitAfterN(string(s), string(sep), int(n)))
}

func (l stringLib) splitN(args ...ref.Val) ref.Val {
	if len(args) != 3 {
		return types.NewErr("no such overload for split_n")
	}
	s, ok := args[0].(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for split_n")
	}
	sep, ok := args[1].(types.String)
	if !ok {
		return types.ValOrErr(sep, "no such overload for split_n")
	}
	n, ok := args[2].(types.Int)
	if !ok {
		return types.ValOrErr(n, "no such overload for split_n")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.SplitN(string(s), string(sep), int(n)))
}

// The obvious test string: 零一二三四五六七八九十
func (l stringLib) substring(args ...ref.Val) ref.Val {
	if len(args) != 3 {
		return types.NewErr("no such overload for substring")
	}
	s, ok := args[0].(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for substring")
	}
	start, ok := args[1].(types.Int)
	if !ok {
		return types.ValOrErr(start, "no such overload for substring")
	}
	if start < 0 {
		return types.NewErr("substring: start out of range: %d < 0", start)
	}
	end, ok := args[2].(types.Int)
	if !ok {
		return types.ValOrErr(end, "no such overload for substring")
	}
	if end < start {
		return types.NewErr("substring: end out of range: %d < %d", end, start)
	}
	i, pos, left := 0, 0, -1
	for ; pos <= len(s); i++ {
		if i == int(start) {
			left = pos
		}
		if i == int(end) {
			// TODO: Consider adding a heuristic to decide if the
			// substring should be cloned to avoid pinning s.
			return types.DefaultTypeAdapter.NativeToValue(s[left:pos])
		}
		if pos == len(s) {
			break
		}
		r, size := utf8.DecodeRuneInString(string(s[pos:]))
		if r == utf8.RuneError {
			return types.NewErr("substring: invalid rune at position %d in string: %s", pos, s)
		}
		pos += size
	}
	if left == -1 {
		return types.NewErr("substring: start out of range: %d > %d", start, i)
	}
	return types.NewErr("substring: end out of range: %d > %d", end, i)
}

func (l stringLib) toLower(arg ref.Val) ref.Val {
	s, ok := arg.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for to_lower")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.ToLower(string(s)))
}

func (l stringLib) toTitle(arg ref.Val) ref.Val {
	s, ok := arg.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for to_title")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.ToTitle(string(s)))
}

func (l stringLib) toUpper(arg ref.Val) ref.Val {
	s, ok := arg.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for to_upper")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.ToUpper(string(s)))
}

func (l stringLib) toValidUTF8(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.Bytes)
	if !ok {
		return types.ValOrErr(s, "no such overload for to_valid_utf8")
	}
	replacement, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(replacement, "no such overload for to_valid_utf8")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.ToValidUTF8(string(s), string(replacement)))
}

func (l stringLib) trim(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for trim")
	}
	cutset, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(cutset, "no such overload for trim")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.Trim(string(s), string(cutset)))
}

func (l stringLib) trimLeft(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for trim_left")
	}
	cutset, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(cutset, "no such overload for trim_left")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.TrimLeft(string(s), string(cutset)))
}

func (l stringLib) trimPrefix(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for trim_prefix")
	}
	prefix, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(prefix, "no such overload for trim_prefix")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.TrimPrefix(string(s), string(prefix)))
}

func (l stringLib) trimRight(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for trim_right")
	}
	cutset, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(cutset, "no such overload for trim_right")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.TrimRight(string(s), string(cutset)))
}

func (l stringLib) trimSpace(arg ref.Val) ref.Val {
	s, ok := arg.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for trim_space")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.TrimSpace(string(s)))
}

func (l stringLib) trimSuffix(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for trim_suffix")
	}
	suffix, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(suffix, "no such overload for trim_suffix")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.TrimSuffix(string(s), string(suffix)))
}

func (l stringLib) validString(arg ref.Val) ref.Val {
	s, ok := arg.(types.Bytes)
	if !ok {
		return types.ValOrErr(s, "no such overload for valid_utf8")
	}
	return types.DefaultTypeAdapter.NativeToValue(utf8.Valid([]byte(s)))
}
