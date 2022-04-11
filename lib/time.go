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
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/google/cel-go/interpreter/functions"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// Time returns a cel.EnvOption to configure extended functions for
// handling timestamps.
//
// Now (function)
//
// Returns a timestamp for when the call was made:
//
//     now() -> <timestamp>
//
// Examples:
//
//     now()  // return "2022-03-30T11:17:57.078390759Z"
//
// Now (Global Variable)
//
//
// Returns a timestamp for when the expression evaluation started:
//
//     now -> <timestamp>
//
// Examples:
//
//     now  // return "2022-03-30T11:17:57.078389559Z"
//
//
// Format
//
// Returns a string representation of the timestamp formatted according to
// the provided layout:
//
//     <timestamp>.format(<string>) -> <string>
//
// Examples:
//
//     now().format(time_layout.Kitchen)  // return "11:17AM"
//
//
// Parse Time
//
// Returns a timestamp from a string based on a time layout or list of possible
// layouts. If a list of formats is provided, the first successful layout is
// used:
//
//     <string>.format(<string>) -> <timestamp>
//     <string>.format(<list<string>>) -> <timestamp>
//
// Examples:
//
//     "11:17AM".parse_time(time_layout.Kitchen)                       // return <timestamp>
//     "11:17AM".parse_time([time_layout.RFC3339,time_layout.Kitchen]) // return <timestamp>
//     "11:17AM".parse_time(time_layout.RFC3339)                       // return error
//
//
// Global Variables
//
// A collection of global variable are provided to give access to the start
// time of the evaluation and to the time formatting layouts provided by
// the Go standard library time package.
//
//     "now": <timestamp of evaluation start>,
//     "time_layout": {
//         "Layout":      time.Layout,
//         "ANSIC":       time.ANSIC,
//         "UnixDate":    time.UnixDate,
//         "RubyDate":    time.RubyDate,
//         "RFC822":      time.RFC822,
//         "RFC822Z":     time.RFC822Z,
//         "RFC850":      time.RFC850,
//         "RFC1123":     time.RFC1123,
//         "RFC1123Z":    time.RFC1123Z,
//         "RFC3339":     time.RFC3339,
//         "RFC3339Nano": time.RFC3339Nano,
//         "Kitchen":     time.Kitchen,
//         "Stamp":       time.Stamp,
//         "StampMilli":  time.StampMilli,
//         "StampMicro":  time.StampMicro,
//         "StampNano":   time.StampNano
//     }
func Time() cel.EnvOption {
	return cel.Lib(timeLib{})
}

type timeLib struct{}

func (timeLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Declarations(
			decls.NewVar("now", decls.Dyn),
			decls.NewVar("time_layout", decls.NewMapType(decls.String, decls.String)),
			decls.NewFunction("now",
				decls.NewOverload(
					"now_void",
					nil,
					decls.Timestamp,
				),
			),
			decls.NewFunction("format",
				decls.NewInstanceOverload(
					"timestamp_format_string",
					[]*expr.Type{decls.Timestamp, decls.String},
					decls.String,
				),
			),
			decls.NewFunction("parse_time",
				decls.NewInstanceOverload(
					"string_parse_time_string",
					[]*expr.Type{decls.String, decls.String},
					decls.Timestamp,
				),
				decls.NewInstanceOverload(
					"string_parse_time_list_string",
					[]*expr.Type{decls.String, decls.NewListType(decls.String)},
					decls.Timestamp,
				),
			),
		),
	}
}

func (timeLib) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{
		cel.Globals(map[string]interface{}{
			"now": func() interface{} { return time.Now().In(time.UTC) },
			"time_layout": map[string]string{
				"Layout":      time.Layout,
				"ANSIC":       time.ANSIC,
				"UnixDate":    time.UnixDate,
				"RubyDate":    time.RubyDate,
				"RFC822":      time.RFC822,
				"RFC822Z":     time.RFC822Z,
				"RFC850":      time.RFC850,
				"RFC1123":     time.RFC1123,
				"RFC1123Z":    time.RFC1123Z,
				"RFC3339":     time.RFC3339,
				"RFC3339Nano": time.RFC3339Nano,
				"Kitchen":     time.Kitchen,
				"Stamp":       time.Stamp,
				"StampMilli":  time.StampMilli,
				"StampMicro":  time.StampMicro,
				"StampNano":   time.StampNano,
			},
		}),
		cel.Functions(
			&functions.Overload{
				Operator: "now_void",
				Function: now,
			},
			&functions.Overload{
				Operator: "timestamp_format_string",
				Binary:   formatTime,
			},
			&functions.Overload{
				Operator: "string_parse_time_string",
				Binary:   parseTimeWithLayout,
			},
			&functions.Overload{
				Operator: "string_parse_time_list_string",
				Binary:   parseTimeWithLayouts,
			},
		),
	}
}

func now(args ...ref.Val) ref.Val {
	if len(args) != 0 {
		return types.NewErr("no such overload")
	}
	return types.Timestamp{Time: time.Now().In(time.UTC)}
}

func formatTime(arg, layout ref.Val) ref.Val {
	obj, ok := arg.(types.Timestamp)
	if !ok {
		return types.ValOrErr(obj, "no such overload for time layout: %s", arg.Type())
	}
	l, ok := layout.(types.String)
	if !ok {
		return types.ValOrErr(l, "no such overload for time layout: %s", layout.Type())
	}
	return types.String(obj.Format(string(l)))
}

func parseTimeWithLayout(arg, layout ref.Val) ref.Val {
	obj, ok := arg.(types.String)
	if !ok {
		return types.ValOrErr(obj, "no such overload for time layout: %s", arg.Type())
	}
	l, ok := layout.(types.String)
	if !ok {
		return types.ValOrErr(l, "no such overload for time layout: %s", layout.Type())
	}
	t, err := time.Parse(string(l), string(obj))
	if err != nil {
		return types.NewErr("failed %v", err)
	}
	return types.Timestamp{Time: t}
}

func parseTimeWithLayouts(arg, layout ref.Val) ref.Val {
	obj, ok := arg.(types.String)
	if !ok {
		return types.ValOrErr(obj, "no such overload for time layout: %s", arg.Type())
	}
	layouts, ok := layout.(traits.Lister)
	if !ok {
		return types.ValOrErr(layouts, "no such overload for time layout: %s", layout.Type())
	}
	it := layouts.Iterator()
	for it.HasNext() == types.True {
		l := it.Next().(types.String)
		t, err := time.Parse(string(l), string(obj))
		if err != nil {
			continue
		}
		return types.Timestamp{Time: t}
	}
	return types.NewErr("failed to parse %s with any provided layout", obj)
}
