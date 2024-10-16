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
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
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
// # String Methods
//
//   - compare: strings.Compare(a, b string) int
//   - contains_substr: strings.Contains(s, substr string) bool
//   - contains_any: strings.ContainsAny(s, chars string) bool
//   - count: strings.Count(s, substr string) int
//   - equal_fold: strings.EqualFold(s, t string) bool
//   - fields: strings.Fields(s string) []string
//   - has_prefix: strings.HasPrefix(s, prefix string) bool
//   - has_suffix: strings.HasSuffix(s, suffix string) bool
//   - index: strings.Index(s, substr string) int
//   - index_any: strings.IndexAny(s, chars string) int
//   - last_index: strings.LastIndex(s, substr string) int
//   - last_index_any: strings.LastIndexAny(s, chars string) int
//   - repeat: strings.Repeat(s string, count int) string
//   - replace: strings.Replace(s, old, new string, n int) string
//   - replace_all: strings.ReplaceAll(s, old, new string) string
//   - split: strings.Split(s, sep string) []string
//   - split_after: strings.SplitAfter(s, sep string) []string
//   - split_after_n: strings.SplitAfterN(s, sep string, n int) []string
//   - split_n: strings.SplitN(s, sep string, n int) []string
//   - to_lower: strings.ToLower(s string) string
//   - to_title: strings.ToTitle(s string) string
//   - to_upper: strings.ToUpper(s string) string
//   - trim: strings.Trim(s, cutset string) string
//   - trim_left: strings.TrimLeft(s, cutset string) string
//   - trim_prefix: strings.TrimPrefix(s, prefix string) string
//   - trim_right: strings.TrimRight(s, cutset string) string
//   - trim_space: strings.TrimSpace(s string) string
//   - trim_suffix: strings.TrimSuffix(s, suffix string) string
//
// In addition to the strings package functions, a sub-string method is provided that allows
// string slicing at unicode code point boundaries. It differs from Go's string slicing
// operator which may slice within a code point resulting in invalid UTF-8. The substring
// method will always return a valid UTF-8 string, or an error if the indexes are out of
// bounds or invalid.
//
//   - substring: s[start:end]
//
// # String List Methods
//
//   - join: strings.Join(elems []string, sep string) string
//
// # Bytes Methods
//
// The to_valid_utf8 method is equivalent to strings.ToValidUTF8 with the receiver first
// converted to a Go string. This special case is required as CEL does not permit invalid
// UTF-8 string conversions.
//
//   - to_valid_utf8: strings.ToValidUTF8(s, replacement string) string
//   - valid_utf8: utf8.Valid(s []byte) bool
//   - compare: bytes.Compare(a, b []byte) int
//   - contains_substr: bytes.Contains(s, substr []byte) bool
//   - has_prefix: bytes.HasPrefix(s, prefix []byte) bool
//   - has_suffix: bytes.HasSuffix(s, suffix []byte) bool
//   - index: bytes.Index(s, substr []byte) int
//   - last_index: bytes.LastIndex(s, substr []byte) int
//   - trim: bytes.Trim(s []byte, cutset string) []byte
//   - trim_left: bytes.TrimLeft(s []byte, cutset string) []byte
//   - trim_prefix: bytes.TrimPrefix(s, prefix []byte) []byte
//   - trim_right: bytes.TrimRight(s []byte, cutset string) []byte
//   - trim_space: bytes.TrimSpace(s []byte) []byte
//   - trim_suffix: bytes.TrimSuffix(s, suffix []byte) []byte
//
// The substring method on bytes slices has the same semantics as the Go byte slice
// slicing operation.
//
//   - substring: s[start:end]
func Strings() cel.EnvOption {
	return cel.Lib(stringLib{})
}

type stringLib struct{}

func (l stringLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function("compare",
			cel.MemberOverload(
				"string_compare_string_int",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.IntType,
				cel.BinaryBinding(l.compare),
			),
			cel.MemberOverload(
				"bytes_compare_bytes_int",
				[]*cel.Type{cel.BytesType, cel.BytesType},
				cel.IntType,
				cel.BinaryBinding(l.compareBytes),
			),
		),
		cel.Function("contains_substr", /* required to disambiguate from regexp.contains.*/
			cel.MemberOverload(
				"string_contains_substr_string_bool",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.BoolType,
				cel.BinaryBinding(l.contains),
			),
			cel.MemberOverload(
				"bytes_contains_substr_bytes_bool",
				[]*cel.Type{cel.BytesType, cel.BytesType},
				cel.BoolType,
				cel.BinaryBinding(l.containsBytes),
			),
		),
		cel.Function("contains_any",
			cel.MemberOverload(
				"string_contains_any_string_bool",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.BoolType,
				cel.BinaryBinding(l.containsAny),
			),
		),
		cel.Function("count",
			cel.MemberOverload(
				"string_count_string_int",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.IntType,
				cel.BinaryBinding(l.count),
			),
		),
		cel.Function("equal_fold",
			cel.MemberOverload(
				"string_equal_fold_string_bool",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.BoolType,
				cel.BinaryBinding(l.equalFold),
			),
		),
		cel.Function("fields",
			cel.MemberOverload(
				"string_fields_list_string",
				[]*cel.Type{cel.StringType},
				listString,
				cel.UnaryBinding(l.fields),
			),
		),
		cel.Function("has_prefix",
			cel.MemberOverload(
				"string_has_prefix_string_bool",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.BoolType,
				cel.BinaryBinding(l.hasPrefix),
			),
			cel.MemberOverload(
				"bytes_has_prefix_bytes_bool",
				[]*cel.Type{cel.BytesType, cel.BytesType},
				cel.BoolType,
				cel.BinaryBinding(l.hasPrefixBytes),
			),
		),
		cel.Function("has_suffix",
			cel.MemberOverload(
				"string_has_suffix_string_bool",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.BoolType,
				cel.BinaryBinding(l.hasSuffix),
			),
			cel.MemberOverload(
				"bytes_has_suffix_bytes_bool",
				[]*cel.Type{cel.BytesType, cel.BytesType},
				cel.BoolType,
				cel.BinaryBinding(l.hasSuffixBytes),
			),
		),
		cel.Function("index",
			cel.MemberOverload(
				"string_index_string_int",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.IntType,
				cel.BinaryBinding(l.index),
			),
			cel.MemberOverload(
				"bytes_index_bytes_int",
				[]*cel.Type{cel.BytesType, cel.BytesType},
				cel.IntType,
				cel.BinaryBinding(l.indexBytes),
			),
		),
		cel.Function("index_any",
			cel.MemberOverload(
				"string_index_any_string_int",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.IntType,
				cel.BinaryBinding(l.indexAny),
			),
		),
		cel.Function("join",
			cel.MemberOverload(
				"list_string_join_string_string",
				[]*cel.Type{listString, cel.StringType},
				cel.StringType,
				cel.BinaryBinding(l.join),
			),
		),
		cel.Function("last_index",
			cel.MemberOverload(
				"string_last_index_string_int",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.IntType,
				cel.BinaryBinding(l.lastIndex),
			),
			cel.MemberOverload(
				"bytes_last_index_bytes_int",
				[]*cel.Type{cel.BytesType, cel.BytesType},
				cel.IntType,
				cel.BinaryBinding(l.lastIndexBytes),
			),
		),
		cel.Function("last_index_any",
			cel.MemberOverload(
				"string_last_index_any_string_int",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.IntType,
				cel.BinaryBinding(l.lastIndexAny),
			),
		),
		cel.Function("repeat",
			cel.MemberOverload(
				"string_repeat_int_string",
				[]*cel.Type{cel.StringType, cel.IntType},
				cel.StringType,
				cel.BinaryBinding(l.repeat),
			),
		),
		cel.Function("replace",
			cel.MemberOverload(
				"string_replace_string_string_int_string",
				[]*cel.Type{cel.StringType, cel.StringType, cel.StringType, cel.IntType},
				cel.StringType,
				cel.FunctionBinding(l.replace),
			),
		),
		cel.Function("replace_all",
			cel.MemberOverload(
				"string_replace_all_string_string_string",
				[]*cel.Type{cel.StringType, cel.StringType, cel.StringType},
				cel.StringType,
				cel.FunctionBinding(l.replaceAll),
			),
		),
		cel.Function("split",
			cel.MemberOverload(
				"string_split_string_list_string",
				[]*cel.Type{cel.StringType, cel.StringType},
				listString,
				cel.BinaryBinding(l.split),
			),
		),
		cel.Function("split_after",
			cel.MemberOverload(
				"string_split_after_string_list_string",
				[]*cel.Type{cel.StringType, cel.StringType},
				listString,
				cel.BinaryBinding(l.splitAfter),
			),
		),
		cel.Function("split_after_n",
			cel.MemberOverload(
				"string_split_after_n_string_int_list_string",
				[]*cel.Type{cel.StringType, cel.StringType, cel.IntType},
				listString,
				cel.FunctionBinding(l.splitAfterN),
			),
		),
		cel.Function("split_n",
			cel.MemberOverload(
				"string_split_n_string_int_list_string",
				[]*cel.Type{cel.StringType, cel.StringType, cel.IntType},
				listString,
				cel.FunctionBinding(l.splitN),
			),
		),
		cel.Function("substring",
			cel.MemberOverload(
				"string_substring_int_int_string",
				[]*cel.Type{cel.StringType, cel.IntType, cel.IntType},
				cel.StringType,
				cel.FunctionBinding(l.substring),
			),
			cel.MemberOverload(
				"bytes_substring_int_int_bytes",
				[]*cel.Type{cel.BytesType, cel.IntType, cel.IntType},
				cel.BytesType,
				cel.FunctionBinding(l.substringBytes),
			),
		),
		cel.Function("to_lower",
			cel.MemberOverload(
				"string_to_lower_string",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(l.toLower),
			),
		),
		cel.Function("to_title",
			cel.MemberOverload(
				"string_to_title_string",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(l.toTitle),
			),
		),
		cel.Function("to_upper",
			cel.MemberOverload(
				"string_to_upper_string",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(l.toUpper),
			),
		),
		cel.Function("to_valid_utf8",
			cel.MemberOverload(
				"bytes_to_valid_utf8_string_string",
				[]*cel.Type{cel.BytesType, cel.StringType},
				cel.StringType,
				cel.BinaryBinding(l.toValidUTF8),
			),
		),
		cel.Function("trim",
			cel.MemberOverload(
				"string_trim_string_string",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.StringType,
				cel.BinaryBinding(l.trim),
			),
			cel.MemberOverload(
				"bytes_trim_bytes_string",
				[]*cel.Type{cel.BytesType, cel.StringType},
				cel.BytesType,
				cel.BinaryBinding(l.trimBytes),
			),
		),
		cel.Function("trim_left",
			cel.MemberOverload(
				"string_trim_left_string_string",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.StringType,
				cel.BinaryBinding(l.trimLeft),
			),
			cel.MemberOverload(
				"bytes_trim_left_bytes_string",
				[]*cel.Type{cel.BytesType, cel.StringType},
				cel.BytesType,
				cel.BinaryBinding(l.trimLeftBytes),
			),
		),
		cel.Function("trim_prefix",
			cel.MemberOverload(
				"string_trim_prefix_string_string",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.StringType,
				cel.BinaryBinding(l.trimPrefix),
			),
			cel.MemberOverload(
				"bytes_trim_prefix_bytes_bytes",
				[]*cel.Type{cel.BytesType, cel.BytesType},
				cel.BytesType,
				cel.BinaryBinding(l.trimPrefixBytes),
			),
		),
		cel.Function("trim_right",
			cel.MemberOverload(
				"string_trim_right_string_string",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.StringType,
				cel.BinaryBinding(l.trimRight),
			),
			cel.MemberOverload(
				"bytes_trim_right_bytes_string",
				[]*cel.Type{cel.BytesType, cel.StringType},
				cel.BytesType,
				cel.BinaryBinding(l.trimRightBytes),
			),
		),
		cel.Function("trim_space",
			cel.MemberOverload(
				"string_trim_space_string",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(l.trimSpace),
			),
			cel.MemberOverload(
				"bytes_trim_space_bytes",
				[]*cel.Type{cel.BytesType},
				cel.BytesType,
				cel.UnaryBinding(l.trimSpaceBytes),
			),
		),
		cel.Function("trim_suffix",
			cel.MemberOverload(
				"string_trim_suffix_string_string",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.StringType,
				cel.BinaryBinding(l.trimSuffix),
			),
			cel.MemberOverload(
				"bytes_trim_suffix_bytes_bytes",
				[]*cel.Type{cel.BytesType, cel.BytesType},
				cel.BytesType,
				cel.BinaryBinding(l.trimSuffixBytes),
			),
		),
		cel.Function("valid_utf8",
			cel.MemberOverload(
				"bytes_valid_utf8_bool",
				[]*cel.Type{cel.BytesType},
				cel.BoolType,
				cel.UnaryBinding(l.validString),
			),
		),
	}
}

func (stringLib) ProgramOptions() []cel.ProgramOption { return nil }

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

func (l stringLib) compareBytes(arg0, arg1 ref.Val) ref.Val {
	a, ok := arg0.(types.Bytes)
	if !ok {
		return types.ValOrErr(a, "no such overload for compare")
	}
	b, ok := arg1.(types.Bytes)
	if !ok {
		return types.ValOrErr(b, "no such overload for compare")
	}
	return types.DefaultTypeAdapter.NativeToValue(bytes.Compare([]byte(a), []byte(b)))
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

func (l stringLib) containsBytes(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.Bytes)
	if !ok {
		return types.ValOrErr(s, "no such overload for contains_substr")
	}
	substr, ok := arg1.(types.Bytes)
	if !ok {
		return types.ValOrErr(substr, "no such overload for contains_substr")
	}
	return types.DefaultTypeAdapter.NativeToValue(bytes.Contains([]byte(s), []byte(substr)))
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

func (l stringLib) hasPrefixBytes(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.Bytes)
	if !ok {
		return types.ValOrErr(s, "no such overload for has_prefix")
	}
	prefix, ok := arg1.(types.Bytes)
	if !ok {
		return types.ValOrErr(prefix, "no such overload for has_prefix")
	}
	return types.DefaultTypeAdapter.NativeToValue(bytes.HasPrefix([]byte(s), []byte(prefix)))
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

func (l stringLib) hasSuffixBytes(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.Bytes)
	if !ok {
		return types.ValOrErr(s, "no such overload for has_suffix")
	}
	suffix, ok := arg1.(types.Bytes)
	if !ok {
		return types.ValOrErr(suffix, "no such overload for has_suffix")
	}
	return types.DefaultTypeAdapter.NativeToValue(bytes.HasSuffix([]byte(s), []byte(suffix)))
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

func (l stringLib) indexBytes(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.Bytes)
	if !ok {
		return types.ValOrErr(s, "no such overload for index")
	}
	substr, ok := arg1.(types.Bytes)
	if !ok {
		return types.ValOrErr(substr, "no such overload for index")
	}
	return types.DefaultTypeAdapter.NativeToValue(bytes.Index([]byte(s), []byte(substr)))
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

func (l stringLib) lastIndexBytes(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.Bytes)
	if !ok {
		return types.ValOrErr(s, "no such overload for last_index")
	}
	substr, ok := arg1.(types.Bytes)
	if !ok {
		return types.ValOrErr(substr, "no such overload for last_index")
	}
	return types.DefaultTypeAdapter.NativeToValue(bytes.LastIndex([]byte(s), []byte(substr)))
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

func (l stringLib) substringBytes(args ...ref.Val) ref.Val {
	if len(args) != 3 {
		return types.NewErr("no such overload for substring")
	}
	s, ok := args[0].(types.Bytes)
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
	return types.DefaultTypeAdapter.NativeToValue(s[start:end])
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

func (l stringLib) trimBytes(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.Bytes)
	if !ok {
		return types.ValOrErr(s, "no such overload for trim")
	}
	cutset, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(cutset, "no such overload for trim")
	}
	return types.DefaultTypeAdapter.NativeToValue(bytes.Trim([]byte(s), string(cutset)))
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

func (l stringLib) trimLeftBytes(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.Bytes)
	if !ok {
		return types.ValOrErr(s, "no such overload for trim_left")
	}
	cutset, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(cutset, "no such overload for trim_left")
	}
	return types.DefaultTypeAdapter.NativeToValue(bytes.TrimLeft([]byte(s), string(cutset)))
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

func (l stringLib) trimPrefixBytes(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.Bytes)
	if !ok {
		return types.ValOrErr(s, "no such overload for trim_prefix")
	}
	prefix, ok := arg1.(types.Bytes)
	if !ok {
		return types.ValOrErr(prefix, "no such overload for trim_prefix")
	}
	return types.DefaultTypeAdapter.NativeToValue(bytes.TrimPrefix([]byte(s), []byte(prefix)))
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

func (l stringLib) trimRightBytes(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.Bytes)
	if !ok {
		return types.ValOrErr(s, "no such overload for trim_right")
	}
	cutset, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(cutset, "no such overload for trim_right")
	}
	return types.DefaultTypeAdapter.NativeToValue(bytes.TrimRight([]byte(s), string(cutset)))
}

func (l stringLib) trimSpace(arg ref.Val) ref.Val {
	s, ok := arg.(types.String)
	if !ok {
		return types.ValOrErr(s, "no such overload for trim_space")
	}
	return types.DefaultTypeAdapter.NativeToValue(strings.TrimSpace(string(s)))
}

func (l stringLib) trimSpaceBytes(arg ref.Val) ref.Val {
	s, ok := arg.(types.Bytes)
	if !ok {
		return types.ValOrErr(s, "no such overload for trim_space")
	}
	return types.DefaultTypeAdapter.NativeToValue(bytes.TrimSpace([]byte(s)))
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

func (l stringLib) trimSuffixBytes(arg0, arg1 ref.Val) ref.Val {
	s, ok := arg0.(types.Bytes)
	if !ok {
		return types.ValOrErr(s, "no such overload for trim_suffix")
	}
	suffix, ok := arg1.(types.Bytes)
	if !ok {
		return types.ValOrErr(suffix, "no such overload for trim_suffix")
	}
	return types.DefaultTypeAdapter.NativeToValue(bytes.TrimSuffix([]byte(s), []byte(suffix)))
}

func (l stringLib) validString(arg ref.Val) ref.Val {
	s, ok := arg.(types.Bytes)
	if !ok {
		return types.ValOrErr(s, "no such overload for valid_utf8")
	}
	return types.DefaultTypeAdapter.NativeToValue(utf8.Valid([]byte(s)))
}

// Printf returns a cel.EnvOption to configure an extended function for
// formatting strings and associated arguments.
//
// # Sprintf
//
// Formats a string from a format string and a set of arguments using the
// Go fmt syntax:
//
//	<string>.sprintf(<list<dyn>>) -> <string>
//	sprintf(<string>, <list<dyn>>) -> <string>
//
// Since CEL does not support variadic functions, arguments are provided
// as an array corresponding to the normal Go variadic slice.
//
// Examples:
//
//	"Hello, %s".sprintf(["World!"])   // return "Hello, World!"
//	sprintf("Hello, %s", ["World!"])  // return "Hello, World!"
func Printf() cel.EnvOption {
	return cel.Lib(printfLib{})
}

type printfLib struct{}

func (l printfLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function("sprintf",
			cel.MemberOverload(
				"sprintf_string_list_string",
				[]*cel.Type{types.StringType, listDyn},
				types.StringType,
				cel.BinaryBinding(l.sprintf),
			),
			cel.Overload(
				"string_sprintf_list_string",
				[]*cel.Type{types.StringType, listDyn},
				types.StringType,
				cel.BinaryBinding(l.sprintf),
			),
		),
	}
}

func (l printfLib) ProgramOptions() []cel.ProgramOption { return nil }

func (l printfLib) sprintf(arg0, arg1 ref.Val) ref.Val {
	format, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(format, "no such overload for sprintf: format is not a string")
	}
	argList, ok := arg1.(traits.Lister)
	if !ok {
		return types.ValOrErr(argList, "no such overload for sprintf: args is not a list")
	}
	n, _ := argList.Size().(types.Int) // Continue since this is an optimisation.
	args := make([]any, 0, n)
	it := argList.Iterator()
	for i := 1; it.HasNext() == types.True; i++ {
		v, err := it.Next().ConvertToNative(reflectAnyType)
		if err != nil {
			return types.NewErr("failed to convert argument %d: %v", i, err)
		}
		args = append(args, v)
	}
	return types.String(fmt.Sprintf(string(format), args...))
}
