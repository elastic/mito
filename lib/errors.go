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
	"github.com/google/cel-go/common"
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
	errs.ReportErrorAtID(id, loc, e.Err.Error())
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
