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
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common"
)

// NewDump returns an evaluation dump that can be used to examine the complete
// set of evaluation states from a CEL program. The program must have been
// constructed with a cel.Env.Program call including the cel.OptTrackState
// evaluation option. The ast and details parameters must be valid for the
// program.
func NewDump(ast *cel.Ast, details *cel.EvalDetails) *Dump {
	if ast == nil || details == nil {
		return nil
	}
	return &Dump{ast: ast, det: details}
}

// Dump is an evaluation dump.
type Dump struct {
	ast *cel.Ast
	det *cel.EvalDetails
}

func (d *Dump) String() string {
	if d == nil {
		return ""
	}
	var buf strings.Builder
	for i, v := range d.NodeValues() {
		if i != 0 {
			buf.WriteByte('\n')
		}
		fmt.Fprint(&buf, v)
	}
	return buf.String()
}

// NodeValues returns the evaluation results, source location and source
// snippets for the expressions in the dump. The nodes are sorted in
// source order.
func (d *Dump) NodeValues() []NodeValue {
	if d == nil {
		return nil
	}
	es := d.det.State()
	var values []NodeValue
	for _, id := range es.IDs() {
		if id == 0 {
			continue
		}
		v, ok := es.Value(id)
		if !ok {
			continue
		}
		values = append(values, d.nodeValue(v, id))
	}
	sort.Slice(values, func(i, j int) bool {
		vi := values[i].loc
		vj := values[j].loc
		switch {
		case vi.Line() < vj.Line():
			return true
		case vi.Line() > vj.Line():
			return false
		}
		switch {
		case vi.Column() < vj.Column():
			return true
		case vi.Column() > vj.Column():
			return false
		default:
			// If we are here we have executed more than once
			// and have different values, so sort lexically.
			// This is not ideal given that values may include
			// maps which do not render consistently and so
			// we're breaking the sort invariant that comparisons
			// will be consistent. For what we are doing this is
			// good enough.
			return fmt.Sprint(values[i].val) < fmt.Sprint(values[j].val)
		}
	})
	return values
}

func (d *Dump) nodeValue(val any, id int64) NodeValue {
	v := NodeValue{
		loc: d.ast.NativeRep().SourceInfo().GetStartLocation(id),
		src: d.ast.Source(),
		val: val,
	}
	return v
}

// NodeValue is a CEL expression node value and annotation.
type NodeValue struct {
	loc common.Location
	src common.Source
	val any
}

func (v NodeValue) MarshalJSON() ([]byte, error) {
	type val struct {
		Location string `json:"loc"`
		Src      string `json:"src"`
		Val      any    `json:"val"`
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	err := enc.Encode(val{
		Location: v.Loc(),
		Src:      v.Src(),
		Val:      v.val,
	})
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (v NodeValue) String() string {
	return fmt.Sprintf("%s\n%s\n%v\n", v.Loc(), v.Src(), v.Val())
}

func (v NodeValue) Val() any {
	return v.val
}

func (v NodeValue) Loc() string {
	return fmt.Sprintf("%s:%d:%d", v.src.Description(), v.loc.Line(), v.loc.Column()+1)
}

func (v NodeValue) Src() string {
	snippet, ok := v.src.Snippet(v.loc.Line())
	if !ok {
		return ""
	}
	src := " | " + strings.Replace(snippet, "\t", " ", -1)
	ind := "\n | " + strings.Repeat(".", minInt(v.loc.Column(), len(snippet))) + "^"
	return src + ind
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
