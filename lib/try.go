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
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter/functions"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// Try returns a cel.EnvOption to configure extended functions for allowing
// errors to be weakened to strings or objects.
//
// # Try
//
// try returns either passes a value through unaltered if it is valid and
// not an error, or it returns a string or object describing the error:
//
//	try(<error>) -> <map<string,string>>
//	try(<dyn>) -> <dyn>
//	try(<error>, <string>) -> <map<string,string>>
//	try(<dyn>, <string>) -> <dyn>
//
// Examples:
//
//	try(0/1)            // return 0
//	try(0/0)            // return "division by zero"
//	try(0/0, "error")   // return {"error": "division by zero"}
//
// # Is Error
//
// is_error returns a bool indicating whether the argument is an error:
//
//	is_error(<dyn>) -> <bool>
//
// Examples:
//
//	is_error(0/1)            // return false
//	is_error(0/0)            // return true
func Try() cel.EnvOption {
	return cel.Lib(tryLib{})
}

type tryLib struct{}

func (tryLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Declarations(
			decls.NewFunction("try",
				decls.NewOverload(
					"try_dyn",
					[]*expr.Type{decls.Dyn},
					decls.Dyn,
				),
				decls.NewOverload(
					"try_dyn_string",
					[]*expr.Type{decls.Dyn, decls.String},
					decls.Dyn,
				),
			),
			decls.NewFunction("is_error",
				decls.NewOverload(
					"is_error_dyn",
					[]*expr.Type{decls.Dyn},
					decls.Bool,
				),
			),
		),
	}
}

func (tryLib) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{
		cel.Functions(
			&functions.Overload{
				Operator:  "try_dyn",
				Unary:     try,
				NonStrict: true,
			},
			&functions.Overload{
				Operator:  "try_dyn_string",
				Binary:    tryMessage,
				NonStrict: true,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator:  "is_error_dyn",
				Unary:     isError,
				NonStrict: true,
			},
		),
	}
}

func try(arg ref.Val) ref.Val {
	if types.IsError(arg) {
		return types.String(fmt.Sprint(arg))
	}
	return arg
}

func tryMessage(arg, msg ref.Val) ref.Val {
	str, ok := msg.(types.String)
	if !ok {
		return types.NoSuchOverloadErr()
	}
	if types.IsError(arg) {
		return types.NewRefValMap(types.DefaultTypeAdapter, map[ref.Val]ref.Val{
			str: types.String(fmt.Sprint(arg)),
		})
	}
	return arg
}

func isError(arg ref.Val) ref.Val {
	return types.Bool(types.IsError(arg))
}
