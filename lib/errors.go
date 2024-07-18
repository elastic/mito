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
	"path"
	"runtime"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// DecoratedError implements error source location rendering.
type DecoratedError struct {
	AST *cel.Ast
	Err error
}

func (e DecoratedError) Error() string {
	if e.Err == nil {
		return "<nil>"
	}

	id, ok := nodeID(e.Err)
	if !ok {
		return e.Err.Error()
	}
	if id == 0 {
		return fmt.Sprintf("%v: unset node id", e.Err)
	}
	if e.AST == nil {
		return fmt.Sprintf("%v: node %d", e.Err, id)
	}
	loc := e.AST.NativeRep().SourceInfo().GetStartLocation(id)
	errs := common.NewErrors(e.AST.Source())
	errs.ReportErrorAtID(id, loc, "%s", e.Err.Error())
	return errs.ToDisplayString()
}

func nodeID(err error) (id int64, ok bool) {
	if err == nil {
		return 0, false
	}

	type nodeIDer interface {
		NodeID() int64
	}

	for {
		if node, ok := err.(nodeIDer); ok {
			return node.NodeID(), true
		}
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			err = x.Unwrap()
			if err == nil {
				return 0, false
			}
		case interface{ Unwrap() []error }:
			for _, err := range x.Unwrap() {
				if node, ok := err.(nodeIDer); ok {
					return node.NodeID(), true
				}
			}
			return 0, false
		default:
			return 0, false
		}
	}
}

type (
	unop  = func(value ref.Val) ref.Val
	binop = func(lhs ref.Val, rhs ref.Val) ref.Val
	varop = func(values ...ref.Val) ref.Val

	bindings interface {
		unop | binop | varop
	}
)

func catch[B bindings](binding B) B {
	switch binding := any(binding).(type) {
	case unop:
		return any(func(arg ref.Val) (ret ref.Val) {
			defer handlePanic(&ret)
			return binding(arg)
		}).(B)
	case binop:
		return any(func(arg0, arg1 ref.Val) (ret ref.Val) {
			defer handlePanic(&ret)
			return binding(arg0, arg1)
		}).(B)
	case varop:
		return any(func(args ...ref.Val) (ret ref.Val) {
			defer handlePanic(&ret)
			return binding(args...)
		}).(B)
	default:
		panic("unreachable")
	}
}

func handlePanic(ret *ref.Val) {
	switch r := recover().(type) {
	case nil:
		return
	default:
		// We'll only try 64 stack frames deep. There are a few recursive
		// functions in extensions, but panic in those functions are unlikely
		// or are already explicitly recovered as part of normal operation.
		pc := make([]uintptr, 64)
		n := runtime.Callers(2, pc)
		cf := runtime.CallersFrames(pc[:n])
		for {
			f, more := cf.Next()
			if !more {
				break
			}
			file := f.File
			if strings.Contains(file, "mito/lib") {
				_, file, _ := strings.Cut(file, "mito/")
				*ret = types.NewErr("%s: %s %s:%d", r, path.Base(f.Function), file, f.Line)
				return
			}
		}
		*ret = types.NewErr("%s", r)
	}
}
