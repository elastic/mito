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

	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// Debug returns a cel.EnvOption to configure extended functions for allowing
// intermediate values to be logged without disrupting program flow.
//
// The debug function will pass errors through without halting the program's
// execution. If the value cannot be serialised, an error will be passed to the
// handler.
//
// # Debug
//
// The second parameter is returned unaltered and the value is logged to the
// lib's logger:
//
//	debug(<string>, <dyn>) -> <dyn>
//
// Examples:
//
//	debug("tag", expr) // return expr even if it is an error and logs with "tag".
func Debug(handler func(tag string, value any)) cel.EnvOption {
	return cel.Lib(debug{handler: handler})
}

type debug struct {
	handler func(string, any)
}

func (l debug) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function("debug",
			cel.Overload(
				"debug_string_dyn",
				[]*cel.Type{cel.StringType, cel.DynType},
				cel.DynType,
				cel.BinaryBinding(l.logDebug),
				cel.OverloadIsNonStrict(),
			),
		),
	}
}

func (debug) ProgramOptions() []cel.ProgramOption { return nil }

func (l debug) logDebug(arg0, arg1 ref.Val) ref.Val {
	tag, ok := arg0.(types.String)
	if !ok {
		return types.ValOrErr(tag, "no such overload")
	}
	if l.handler == nil {
		return arg1
	}
	val, err := arg1.ConvertToNative(reflect.TypeOf((*structpb.Value)(nil)))
	if err != nil {
		l.handler(string(tag), err)
	} else {
		switch val := val.(type) {
		case *structpb.Value:
			l.handler(string(tag), val.AsInterface())
		default:
			// This should never happen.
			l.handler(string(tag), val)
		}
	}
	return arg1
}
