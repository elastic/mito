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
	"reflect"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/operators"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/google/cel-go/interpreter/functions"
	"github.com/google/cel-go/parser"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// Collections returns a cel.EnvOption to configure extended functions for
// handling collections.
//
// As (Macro)
//
// The as macro is syntactic sugar for [val].map(var, function)[0].
//
// Examples:
//
//	{"a":1, "b":2}.as(v, v.a == 1)         // return true
//	{"a":1, "b":2}.as(v, v)                // return {"a":1, "b":2}
//	{"a":1, "b":2}.as(v, v.with({"c":3}))  // return {"a":1, "b":2, "c":3}
//	{"a":1, "b":2}.as(v, [v, v])           // return [{"a":1, "b":2}, {"a":1, "b":2}]
//
// # Collate
//
// Returns a list of values obtained by traversing fields in the receiver with
// the path or paths given as the string parameter. When a list is traversed all
// children are include in the resulting list:
//
//	<list<dyn>>.collate(<string>) -> <list<dyn>>
//	<list<dyn>>.collate(<list<string>>) -> <list<dyn>>
//	<map<string,dyn>>.collate(<string>) -> <list<dyn>>
//	<map<string,dyn>>.collate(<list<string>>) -> <list<dyn>>
//
// Examples:
//
//	Given v:
//	{
//	        "a": [
//	            {"b": 1},
//	            {"b": 2},
//	            {"b": 3}
//	        ],
//	        "b": [
//	            {"b": -1, "c": 10},
//	            {"b": -2, "c": 20},
//	            {"b": -3, "c": 30}
//	        ]
//	}
//
//	v.collate("a")             // return [{"b": 1}, {"b": 2}, {"b": 3}]
//	v.collate("a.b")           // return [1, 2, 3]
//	v.collate(["a.b", "b.b"])  // return [1, 2, 3, -1, -2, -3]
//	v.collate(["a", "b.b"])    // return [{"b": 1 }, {"b": 2 }, {"b": 3 }, -1, -2, -3 ]
//
// If the the path to be dropped includes a dot, it can be escaped with a literal
// backslash. See drop below.
//
// # Drop
//
// Returns the value of the receiver with the object at the given paths remove:
//
//	<list<dyn>>.drop(<string>) -> <list<dyn>>
//	<list<dyn>>.drop(<list<string>>) -> <list<dyn>>
//	<map<string,dyn>>.drop(<string>) -> <map<string,dyn>>
//	<map<string,dyn>>.drop(<list<string>>) -> <map<string,dyn>>
//
// Examples:
//
//	Given v:
//	{
//	        "a": [
//	            {"b": 1},
//	            {"b": 2},
//	            {"b": 3}
//	        ],
//	        "b": [
//	            {"b": -1, "c": 10},
//	            {"b": -2, "c": 20},
//	            {"b": -3, "c": 30}
//	        ]
//	}
//
//	v.drop("a")             // return {"b": [{"b": -1, "c": 10}, {"b": -2, "c": 20}, {"b": -3, "c": 30}]}
//	v.drop("a.b")           // return {"a": [{}, {}, {}], "b": [{"b": -1, "c": 10}, {"b": -2, "c": 20}, {"b": -3, "c": 30}]}
//	v.drop(["a.b", "b.b"])  // return {"a": [{}, {}, {}], "b": [{"c": 10}, {"c": 20}, {"c": 30}]}
//	v.drop(["a", "b.b"])    // return {"b": [{"c": 10}, {"c": 20}, {"c": 30}]}
//
// If the the path to be dropped includes a dot, it can be escaped with a literal
// backslash.
//
// Examples:
//
//	Given v:
//	{
//	        "dotted.path": [
//	            {"b": -1, "c": 10},
//	            {"b": -2, "c": 20},
//	            {"b": -3, "c": 30}
//	        ]
//	}
//
//	v.drop("dotted\\.path.b")  // return {"dotted.path": [{"c": 10}, {"c": 20}, {"c": 30}]}
//
// # Drop Empty
//
// Returns the value of the receiver with all empty lists and maps removed,
// recursively
//
//	<list<dyn>>.drop_empty() -> <list<dyn>>
//	<map<string,dyn>>.drop_empty() -> <map<string,dyn>>
//
// Examples:
//
//	Given v:
//	{
//	        "a": [
//	            {},
//	            {},
//	            {}
//	        ],
//	        "b": [
//	            {"b": -1, "c": 10},
//	            {"b": -2, "c": 20},
//	            {"b": -3, "c": 30}
//	        ]
//	}
//
//	v.drop_empty()  // return {"b":[{"b":-1, "c":10}, {"b":-2, "c":20}, {"b":-3, "c":30}]}
//
// # Flatten
//
// Returns a list of non-list objects resulting from the depth-first
// traversal of a nested list:
//
//	<list<dyn>...>.flatten() -> <list<dyn>>
//
// Examples:
//
//	[[1],[2,3],[[[4]],[5,6]]].flatten()                     // return [1, 2, 3, 4, 5, 6]
//	[[{"a":1,"b":[10, 11]}],[2,3],[[[4]],[5,6]]].flatten()  // return [{"a":1, "b":[10, 11]}, 2, 3, 4, 5, 6]
//
// # Max
//
// Returns the maximum value of a list of comparable objects:
//
//	<list<dyn>>.max() -> <dyn>
//	max(<list<dyn>>) -> <dyn>
//
// Examples:
//
//	[1,2,3,4,5,6,7].max()  // return 7
//	max([1,2,3,4,5,6,7])   // return 7
//
// # Min
//
// Returns the minimum value of a list of comparable objects:
//
//	<list<dyn>>.min() -> <dyn>
//	min(<list<dyn>>) -> <dyn>
//
// Examples:
//
//	[1,2,3,4,5,6,7].min()  // return 1
//	min([1,2,3,4,5,6,7])   // return 1
//
// # With
//
// Returns the receiver's value with the value of the parameter updating
// or adding fields:
//
//	<map<K,V>>.with(<map<K,V>>) -> <map<K,V>>
//
// Examples:
//
//	{"a":1, "b":2}.with({"a":10, "c":3})  // return {"a":10, "b":2, "c":3}
//
// # With Replace
//
// Returns the receiver's value with the value of the parameter replacing
// existing fields:
//
//	<map<K,V>>.with(<map<K,V>>) -> <map<K,V>>
//
// Examples:
//
//	{"a":1, "b":2}.with({"a":10, "c":3})  // return {"a":10, "b":2}
//
// # With Update
//
// Returns the receiver's value with the value of the parameter updating
// the map without replacing any existing fields:
//
//	<map<K,V>>.with(<map<K,V>>) -> <map<K,V>>
//
// Examples:
//
//	{"a":1, "b":2}.with({"a":10, "c":3})  // return {"a":1, "b":2, "c":3}
func Collections() cel.EnvOption {
	return cel.Lib(collectionsLib{})
}

type collectionsLib struct{}

func (collectionsLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Macros(parser.NewReceiverMacro("as", 2, makeAs)),
		cel.Declarations(
			decls.NewFunction("collate",
				decls.NewParameterizedInstanceOverload(
					"list_collate_string",
					[]*expr.Type{decls.NewListType(decls.Dyn), decls.String},
					listV,
					[]string{"V"},
				),
				decls.NewParameterizedInstanceOverload(
					"list_collate_list_string",
					[]*expr.Type{decls.NewListType(decls.Dyn), decls.NewListType(decls.String)},
					listV,
					[]string{"V"},
				),
				decls.NewParameterizedInstanceOverload(
					"map_collate_string",
					[]*expr.Type{mapStringDyn, decls.String},
					listV,
					[]string{"V"},
				),
				decls.NewParameterizedInstanceOverload(
					"map_collate_list_string",
					[]*expr.Type{mapStringDyn, decls.NewListType(decls.String)},
					listV,
					[]string{"V"},
				),
			),
			decls.NewFunction("drop",
				decls.NewInstanceOverload(
					"list_drop_string",
					[]*expr.Type{decls.NewListType(decls.Dyn), decls.String},
					decls.NewListType(decls.Dyn),
				),
				decls.NewInstanceOverload(
					"list_drop_list_string",
					[]*expr.Type{decls.NewListType(decls.Dyn), decls.NewListType(decls.String)},
					decls.NewListType(decls.Dyn),
				),
				decls.NewInstanceOverload(
					"map_drop_string",
					[]*expr.Type{mapKV, decls.String},
					mapKV,
				),
				decls.NewInstanceOverload(
					"map_drop_list_string",
					[]*expr.Type{mapKV, decls.NewListType(decls.String)},
					mapKV,
				),
			),
			decls.NewFunction("drop_empty",
				decls.NewInstanceOverload(
					"list_drop_empty",
					[]*expr.Type{decls.NewListType(decls.Dyn)},
					decls.NewListType(decls.Dyn),
				),
				decls.NewInstanceOverload(
					"map_drop_empty",
					[]*expr.Type{mapKV},
					mapKV,
				),
			),
			decls.NewFunction("flatten",
				decls.NewInstanceOverload(
					"list_flatten",
					[]*expr.Type{decls.NewListType(decls.Dyn)},
					decls.NewListType(decls.Dyn),
				),
			),
			decls.NewFunction("max",
				decls.NewParameterizedInstanceOverload(
					"list_max",
					[]*expr.Type{listV},
					typeV,
					[]string{"V"},
				),
				decls.NewParameterizedOverload(
					"max_list",
					[]*expr.Type{listV},
					typeV,
					[]string{"V"},
				),
			),
			decls.NewFunction("min",
				decls.NewParameterizedInstanceOverload(
					"list_min",
					[]*expr.Type{listV},
					typeV,
					[]string{"V"},
				),
				decls.NewParameterizedOverload(
					"min_list",
					[]*expr.Type{listV},
					typeV,
					[]string{"V"},
				),
			),
			decls.NewFunction("with",
				decls.NewParameterizedInstanceOverload(
					"map_with_map",
					[]*expr.Type{mapKV, mapKV},
					mapKV,
					[]string{"K", "V"},
				),
			),
			decls.NewFunction("with_update",
				decls.NewParameterizedInstanceOverload(
					"map_with_update_map",
					[]*expr.Type{mapKV, mapKV},
					mapKV,
					[]string{"K", "V"},
				),
			),
			decls.NewFunction("with_replace",
				decls.NewParameterizedInstanceOverload(
					"map_with_replace_map",
					[]*expr.Type{mapKV, mapKV},
					mapKV,
					[]string{"K", "V"},
				),
			),
		),
	}
}

func (collectionsLib) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{
		cel.Functions(
			&functions.Overload{
				Operator: "list_collate_string",
				Binary:   collateFields,
			},
			&functions.Overload{
				Operator: "list_collate_list_string",
				Binary:   collateFields,
			},
			&functions.Overload{
				Operator: "map_collate_string",
				Binary:   collateFields,
			},
			&functions.Overload{
				Operator: "map_collate_list_string",
				Binary:   collateFields,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "list_drop_string",
				Binary:   dropFields,
			},
			&functions.Overload{
				Operator: "list_drop_list_string",
				Binary:   dropFields,
			},
			&functions.Overload{
				Operator: "map_drop_string",
				Binary:   dropFields,
			},
			&functions.Overload{
				Operator: "map_drop_list_string",
				Binary:   dropFields,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "list_drop_empty",
				Unary:    dropEmpty,
			},
			&functions.Overload{
				Operator: "map_drop_empty",
				Unary:    dropEmpty,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "list_flatten",
				Unary:    flatten,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "min_list",
				Unary:    min,
			},
			&functions.Overload{
				Operator: "list_min",
				Unary:    min,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "max_list",
				Unary:    max,
			},
			&functions.Overload{
				Operator: "list_max",
				Unary:    max,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "map_with_map",
				Binary:   withAll,
			},
			&functions.Overload{
				Operator: "map_with_update_map",
				Binary:   withUpdate,
			},
			&functions.Overload{
				Operator: "map_with_replace_map",
				Binary:   withReplace,
			},
		),
	}
}

func flatten(arg ref.Val) ref.Val {
	obj := arg
	l, ok := obj.(traits.Lister)
	if !ok {
		return types.ValOrErr(obj, "no such overload")
	}
	dst := types.NewMutableList(types.DefaultTypeAdapter)
	flattenParts(dst, l)
	return dst.ToImmutableList()
}

func flattenParts(dst traits.MutableLister, val traits.Lister) {
	it := val.Iterator()
	for it.HasNext().Value().(bool) {
		if _, ok := it.Next().(traits.Lister); !ok {
			dst.Add(val)
			return
		}
	}
	it = val.Iterator()
	for it.HasNext() == types.True {
		flattenParts(dst, it.Next().(traits.Lister))
	}
}

func withAll(dst, src ref.Val) ref.Val {
	new, other, err := with(dst, src)
	if err != nil {
		return err
	}
	for k, v := range other {
		new[k] = v
	}
	return types.NewRefValMap(types.DefaultTypeAdapter, new)
}

func withUpdate(dst, src ref.Val) ref.Val {
	new, other, err := with(dst, src)
	if err != nil {
		return err
	}
	for k, v := range other {
		if _, ok := new[k]; ok {
			continue
		}
		new[k] = v
	}
	return types.NewRefValMap(types.DefaultTypeAdapter, new)
}

func withReplace(dst, src ref.Val) ref.Val {
	new, other, err := with(dst, src)
	if err != nil {
		return err
	}
	for k, v := range other {
		if _, ok := new[k]; !ok {
			continue
		}
		new[k] = v
	}
	return types.NewRefValMap(types.DefaultTypeAdapter, new)
}

var refValMap = reflect.TypeOf(map[ref.Val]ref.Val(nil))

func with(dst, src ref.Val) (res, other map[ref.Val]ref.Val, maybe ref.Val) {
	obj, ok := dst.(traits.Mapper)
	if !ok {
		return nil, nil, types.ValOrErr(obj, "no such overload")
	}
	val, ok := src.(traits.Mapper)
	if !ok {
		return nil, nil, types.ValOrErr(src, "unsupported src type")
	}

	new := make(map[ref.Val]ref.Val)
	m, err := obj.ConvertToNative(refValMap)
	if err != nil {
		return nil, nil, types.NewErr("unable to convert dst to native: %v", err)
	}
	for k, v := range m.(map[ref.Val]ref.Val) {
		new[k] = v
	}
	m, err = val.ConvertToNative(refValMap)
	if err != nil {
		return nil, nil, types.NewErr("unable to convert src to native: %v", err)
	}
	return new, m.(map[ref.Val]ref.Val), nil
}

// TODO: Make this configurable to allow map, list and string emptiness and null.
func dropEmpty(val ref.Val) ref.Val {
	obj, ok := val.(iterator)
	if !ok || !hasEmpty(obj) {
		return val
	}

	switch obj := val.(type) {
	case traits.Lister:
		new := make([]ref.Val, 0, obj.Size().Value().(int64))
		it := obj.Iterator()
		for it.HasNext() == types.True {
			elem := it.Next()
			switch val := elem.(type) {
			case iterator:
				if val.Size() != types.IntZero {
					res := dropEmpty(val)
					if v, ok := res.(traits.Sizer); ok {
						if v.Size() != types.IntZero {
							new = append(new, res)
						}
					} else {
						new = append(new, res)
					}
				}
			default:
				new = append(new, val)
			}
		}
		return types.NewRefValList(types.DefaultTypeAdapter, new)

	case traits.Mapper:
		new := make(map[ref.Val]ref.Val)
		m, err := obj.ConvertToNative(refValMap)
		if err != nil {
			return types.NewErr("unable to convert map to native: %v", err)
		}
		for k, v := range m.(map[ref.Val]ref.Val) {
			switch val := v.(type) {
			case iterator:
				if val.Size() != types.IntZero {
					res := dropEmpty(v)
					if v, ok := res.(traits.Sizer); ok {
						if v.Size() != types.IntZero {
							new[k] = res
						}
					} else {
						new[k] = res
					}
				}
			default:
				new[k] = v
			}
		}
		return types.NewRefValMap(types.DefaultTypeAdapter, new)

	default:
		// This should never happen since non-iterator
		// types will have been returned in the preamble.
		return val
	}
}

// hasEmpty returns whether val is a map or a list that has any zero-sized
// map or list elements recursively. Zero sized strings are not considered
// to be empty.
func hasEmpty(val iterator) bool {
	it := val.Iterator()
	switch val := val.(type) {
	case traits.Lister:
		for it.HasNext() == types.True {
			elem := it.Next()
			iter, ok := elem.(iterator)
			if !ok {
				continue
			}
			if iter.Size() == types.IntZero || hasEmpty(iter) {
				return true
			}
		}
	case traits.Mapper:
		for it.HasNext() == types.True {
			elem := val.Get(it.Next())
			iter, ok := elem.(iterator)
			if !ok {
				continue
			}
			if iter.Size() == types.IntZero || hasEmpty(iter) {
				return true
			}
		}
	}
	return false
}

// iterator is the common interface for lists and maps required for dropEmpty.
type iterator interface {
	ref.Val
	traits.Iterable
	traits.Sizer
}

func dropFields(obj, fields ref.Val) ref.Val {
	switch fields := fields.(type) {
	case types.String:
		return dropFieldPath(obj, fields)
	case traits.Lister:
		it := fields.Iterator()
		for it.HasNext() == types.True {
			obj = dropFieldPath(obj, it.Next().ConvertToType(types.StringType).(types.String))
		}
		return obj
	}
	return types.NewErr("invalid parameter type for drop: %v", fields.Type())
}

func dropFieldPath(arg ref.Val, path types.String) (val ref.Val) {
	defer func() {
		switch err := recover().(type) {
		case *types.Err:
			val = err
		}
	}()
	if !hasFieldPath(arg, path) {
		return arg
	}

	switch obj := arg.(type) {
	case traits.Lister:
		new := make([]ref.Val, 0, obj.Size().Value().(int64))
		it := obj.Iterator()
		for it.HasNext() == types.True {
			elem := it.Next()
			new = append(new, dropFieldPath(elem, path))
		}
		return types.NewRefValList(types.DefaultTypeAdapter, new)

	case traits.Mapper:
		dotIdx, escaped := pathSepIndex(string(path))
		switch {
		case dotIdx == 0, dotIdx == len(path)-1:
			return types.NewErr("invalid parameter path for drop: %s", path)

		case dotIdx < 0:
			new := make(map[ref.Val]ref.Val)
			m, err := obj.ConvertToNative(refValMap)
			if err != nil {
				return types.NewErr("unable to convert map to native: %v", err)
			}
			for k, v := range m.(map[ref.Val]ref.Val) {
				if k.Equal(path) == types.False {
					new[k] = v
				}
			}
			return types.NewRefValMap(types.DefaultTypeAdapter, new)

		default:
			new := make(map[ref.Val]ref.Val)
			m, err := obj.ConvertToNative(refValMap)
			if err != nil {
				return types.NewErr("unable to convert map to native: %v", err)
			}
			head := path[:dotIdx]
			if escaped {
				head = types.String(strings.ReplaceAll(string(head), `\.`, "."))
			}
			tail := path[dotIdx+1:]
			for k, v := range m.(map[ref.Val]ref.Val) {
				if k.Equal(head) == types.True {
					new[head] = dropFieldPath(v, tail)
				} else {
					new[k] = v
				}
			}
			return types.NewRefValMap(types.DefaultTypeAdapter, new)
		}

	default:
		return obj
	}
}

func hasFieldPath(arg ref.Val, path types.String) bool {
	switch obj := arg.(type) {
	case traits.Lister:
		it := obj.Iterator()
		for it.HasNext() == types.True {
			if hasFieldPath(it.Next(), path) {
				return true
			}
		}
		return false

	case traits.Mapper:
		dotIdx, escaped := pathSepIndex(string(path))
		switch {
		case dotIdx == 0, dotIdx == len(path)-1:
			panic(types.NewErr("invalid parameter path for drop: %s", path))

		case dotIdx < 0:
			m, err := obj.ConvertToNative(refValMap)
			if err != nil {
				panic(types.NewErr("unable to convert map to native: %v", err))
			}
			for k := range m.(map[ref.Val]ref.Val) {
				if k.Equal(path) == types.True {
					return true
				}
			}
			return false

		default:
			m, err := obj.ConvertToNative(refValMap)
			if err != nil {
				panic(types.NewErr("unable to convert map to native: %v", err))
			}
			head := path[:dotIdx]
			if escaped {
				head = types.String(strings.ReplaceAll(string(head), `\.`, "."))
			}
			tail := path[dotIdx+1:]
			for k, v := range m.(map[ref.Val]ref.Val) {
				if k.Equal(head) == types.True {
					return hasFieldPath(v, tail)
				}
			}
			return false
		}

	default:
		return false
	}
}

func collateFields(arg, fields ref.Val) (vals ref.Val) {
	defer func() {
		switch err := recover().(type) {
		case *types.Err:
			vals = err
		}
	}()
	switch fields := fields.(type) {
	case types.String:
		return types.NewRefValList(types.DefaultTypeAdapter, collateFieldPath(arg, fields))
	case traits.Lister:
		var elems []ref.Val
		it := fields.Iterator()
		for it.HasNext() == types.True {
			switch field := it.Next().(type) {
			case types.String:
				elems = append(elems, collateFieldPath(arg, field.ConvertToType(types.StringType).(types.String))...)
			default:
				return types.NewErr("invalid parameter type for collate fields: %v", field.Type())
			}
		}
		return types.NewRefValList(types.DefaultTypeAdapter, elems)
	}
	return types.NewErr("invalid parameter type for collate: %v", fields.Type())
}

func collateFieldPath(arg ref.Val, path types.String) []ref.Val {
	var collation []ref.Val
	switch obj := arg.(type) {
	case traits.Lister:
		it := obj.Iterator()
		for it.HasNext() == types.True {
			elem := it.Next()
			collation = append(collation, collateFieldPath(elem, path)...)
		}
		return collation

	case traits.Mapper:
		dotIdx, escaped := pathSepIndex(string(path))
		switch {
		case dotIdx == 0, dotIdx == len(path)-1:
			panic(types.NewErr("invalid parameter path for drop: %s", path))

		case dotIdx < 0:
			m, err := obj.ConvertToNative(refValMap)
			if err != nil {
				panic(types.NewErr("unable to convert map to native: %v", err))
			}
			for k, v := range m.(map[ref.Val]ref.Val) {
				if k.Equal(path) == types.True {
					switch v := v.(type) {
					case traits.Lister:
						it := v.Iterator()
						for it.HasNext() == types.True {
							collation = append(collation, it.Next())
						}
					default:
						collation = append(collation, v)
					}
				}
			}

		default:
			m, err := obj.ConvertToNative(refValMap)
			if err != nil {
				panic(types.NewErr("unable to convert map to native: %v", err))
			}
			head := path[:dotIdx]
			if escaped {
				head = types.String(strings.ReplaceAll(string(head), `\.`, "."))
			}
			tail := path[dotIdx+1:]
			for k, v := range m.(map[ref.Val]ref.Val) {
				if k.Equal(head) == types.True {
					collation = append(collation, collateFieldPath(v, tail)...)
				}
			}
		}

	default:
		if path == "" {
			collation = []ref.Val{obj}
		}
	}

	return collation
}

func min(arg ref.Val) ref.Val {
	return compare(arg, -1)
}

func max(arg ref.Val) ref.Val {
	return compare(arg, 1)
}

func compare(arg ref.Val, cmp types.Int) ref.Val {
	list, ok := arg.(traits.Lister)
	if !ok {
		return types.NoSuchOverloadErr()
	}

	type comparer interface {
		ref.Val
		traits.Comparer
	}
	var min comparer
	it := list.Iterator()
	for it.HasNext() == types.True {
		elem, ok := it.Next().(comparer)
		if !ok {
			return types.NoSuchOverloadErr()
		}
		if min == nil || elem.Compare(min) == cmp {
			min = elem
		}
	}
	return min
}

func makeAs(eh parser.ExprHelper, target *expr.Expr, args []*expr.Expr) (*expr.Expr, *common.Error) {
	ident := args[0]
	if _, ok := ident.ExprKind.(*expr.Expr_IdentExpr); !ok {
		return nil, &common.Error{Message: "argument is not an identifier"}
	}
	label := ident.GetIdentExpr().GetName()

	fn := args[1]
	target = eh.NewList(target) // Fold is a list comprehension, so fake this.
	accuExpr := eh.Ident(parser.AccumulatorName)
	init := eh.NewList() // Also for the result.
	condition := eh.LiteralBool(true)
	step := eh.GlobalCall(operators.Add, accuExpr, eh.NewList(fn))
	fold := eh.Fold(label, target, parser.AccumulatorName, init, condition, step, accuExpr)
	return eh.GlobalCall(operators.Index, fold, eh.LiteralInt(0)), nil
}

// pathSepIndex returns the offset to a non-escaped dot path separator and
// whether the path element before the separator contains a backslash-escaped
// path separator.
func pathSepIndex(s string) (off int, escaped bool) {
	for {
		idx := strings.IndexByte(s[off:], '.')
		if idx == -1 {
			return -1, escaped
		}
		off += idx
		if idx == 0 || s[off-1] != '\\' {
			return off, escaped
		}
		off++
		escaped = true
	}
}
