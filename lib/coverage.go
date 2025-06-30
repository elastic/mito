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
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter"
)

// NewCoverage return an execution coverage statistics collector for the
// provided AST.
func NewCoverage(ast *cel.Ast) *Coverage {
	return &Coverage{
		ast: ast,
		decorator: coverage{
			all: make(map[int64]bool),
			cov: make(map[int64]bool),
		},
	}
}

// Coverage is a CEL program execution coverage statistics collector.
type Coverage struct {
	ast       *cel.Ast
	decorator coverage
}

// ProgramOption return a cel.ProgramOption that can be used in a call to
// cel.Env.Program to collect coverage information from the program's execution.
func (c *Coverage) ProgramOption() cel.ProgramOption {
	return cel.CustomDecorator(func(i interpreter.Interpretable) (interpreter.Interpretable, error) {
		c.decorator.all[i.ID()] = true
		switch i := i.(type) {
		case interpreter.InterpretableAttribute:
			return coverageAttribute{InterpretableAttribute: i, cov: c.decorator.cov}, nil
		case interpreter.InterpretableCall:
			return coverageCall{InterpretableCall: i, cov: c.decorator.cov}, nil
		case interpreter.InterpretableConst:
			return coverageConst{InterpretableConst: i, cov: c.decorator.cov}, nil
		case interpreter.InterpretableConstructor:
			return coverageConstructor{InterpretableConstructor: i, cov: c.decorator.cov}, nil
		default:
			// Check that we do not have more methods on the original
			// type than the base interpreter.Interpretable type. In
			// the case that we hit this, the program will probably
			// run, but may give unexpected results.
			var ii interpreter.Interpretable
			if exportedMethods(reflect.TypeOf(i)) > exportedMethods(reflect.TypeOf(&ii).Elem()) {
				return nil, fmt.Errorf("unsupported interpretable type: %T", i)
			}
			return coverage{Interpretable: i, cov: c.decorator.cov}, nil
		}
	})
}

func exportedMethods(typ reflect.Type) int {
	var n int
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		if m.IsExported() {
			n++
		}
	}
	return n
}

func (c *Coverage) String() string {
	var buf bytes.Buffer
	for i, d := range c.Details() {
		if i != 0 {
			fmt.Fprintln(&buf)
		}
		fmt.Fprintf(&buf, "%s", d)
	}
	return buf.String()
}

// Merge adds node coverage from o into c. If c was constructed with NewCoverage
// o and c must have been constructed with the AST from the same source. If o is
// nil, Merge is a no-op.
func (c *Coverage) Merge(o *Coverage) error {
	if o == nil {
		return nil
	}
	if c.decorator.all == nil {
		*c = *o
		return nil
	}
	if !equalNodes(c.decorator.all, o.decorator.all) {
		return errors.New("cannot merge unrelated coverage: mismatched nodes")
	}
	if c.ast.Source().Content() != o.ast.Source().Content() {
		return errors.New("cannot merge unrelated coverage: mismatched source")
	}
	for id := range o.decorator.cov {
		c.decorator.cov[id] = true
	}
	return nil
}

func equalNodes(a, b map[int64]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v1 := range a {
		if v2, ok := b[k]; !ok || v1 != v2 {
			return false
		}
	}
	return true
}

// Details returns the coverage details from running the target CEL program.
func (c *Coverage) Details() []LineCoverage {
	nodes := make(map[int][]int64)
	for id := range c.decorator.all {
		line := c.ast.NativeRep().SourceInfo().GetStartLocation(id).Line()
		nodes[line] = append(nodes[line], id)
	}
	hits := make(map[int][]int64)
	for id := range c.decorator.cov {
		line := c.ast.NativeRep().SourceInfo().GetStartLocation(id).Line()
		hits[line] = append(hits[line], id)
	}
	stats := make(map[int]float64)
	var lines []int
	for l := range nodes {
		sort.Slice(nodes[l], func(i, j int) bool { return nodes[l][i] < nodes[l][j] })
		sort.Slice(hits[l], func(i, j int) bool { return hits[l][i] < hits[l][j] })
		stats[l] = float64(len(hits[l])) / float64(len(nodes[l]))
		lines = append(lines, l)
	}
	sort.Ints(lines)
	cov := make([]LineCoverage, 0, len(lines))
	src := c.ast.Source()
	for _, l := range lines {
		var missed []int64
		i, j := 0, 0
		for i < len(nodes[l]) && j < len(hits[l]) {
			if nodes[l][i] == hits[l][j] {
				i++
				j++
				continue
			}
			missed = append(missed, nodes[l][i])
			i++
		}
		missed = append(missed, nodes[l][i:]...)
		cov = append(cov, LineCoverage{
			Line:       l,
			Coverage:   stats[l],
			Nodes:      nodes[l],
			Covered:    hits[l],
			Missed:     missed,
			Annotation: srcAnnot(c.ast, src, missed, "!"),
		})
	}
	return cov
}

func srcAnnot(ast *cel.Ast, src common.Source, nodes []int64, mark string) string {
	if len(nodes) == 0 {
		return ""
	}
	var buf bytes.Buffer
	columns := make(map[int]bool)
	var snippet string
	for _, id := range nodes {
		loc := ast.NativeRep().SourceInfo().GetStartLocation(id)
		if columns[loc.Column()] {
			continue
		}
		columns[loc.Column()] = true
		if snippet == "" {
			var ok bool
			snippet, ok = src.Snippet(loc.Line())
			if !ok {
				continue
			}
		}
	}
	missed := make([]int, 0, len(columns))
	for col := range columns {
		missed = append(missed, col)
	}
	sort.Ints(missed)
	fmt.Fprintln(&buf, " | "+strings.Replace(snippet, "\t", " ", -1))
	fmt.Fprint(&buf, " | ")
	var last int
	for _, col := range missed {
		fmt.Fprint(&buf, strings.Repeat(" ", minInt(col, len(snippet))-last)+mark)
		last = col + 1
	}
	return buf.String()
}

type coverage struct {
	interpreter.Interpretable
	all map[int64]bool
	cov map[int64]bool
}

func (c coverage) Eval(a interpreter.Activation) ref.Val {
	c.cov[c.ID()] = true
	return c.Interpretable.Eval(a)
}

type coverageAttribute struct {
	interpreter.InterpretableAttribute
	all map[int64]bool
	cov map[int64]bool
}

func (c coverageAttribute) Eval(a interpreter.Activation) ref.Val {
	c.cov[c.ID()] = true
	return c.InterpretableAttribute.Eval(a)
}

type coverageCall struct {
	interpreter.InterpretableCall
	all map[int64]bool
	cov map[int64]bool
}

func (c coverageCall) Eval(a interpreter.Activation) ref.Val {
	c.cov[c.ID()] = true
	return c.InterpretableCall.Eval(a)
}

type coverageConst struct {
	interpreter.InterpretableConst
	all map[int64]bool
	cov map[int64]bool
}

func (c coverageConst) Eval(a interpreter.Activation) ref.Val {
	c.cov[c.ID()] = true
	return c.InterpretableConst.Eval(a)
}

type coverageConstructor struct {
	interpreter.InterpretableConstructor
	all map[int64]bool
	cov map[int64]bool
}

func (c coverageConstructor) Eval(a interpreter.Activation) ref.Val {
	c.cov[c.ID()] = true
	return c.InterpretableConstructor.Eval(a)
}

// LineCoverage is the execution coverage data for a single line of a CEL
// program.
type LineCoverage struct {
	// Line is the line number of the program.
	Line int `json:"line"`
	// Coverage is the fraction of CEL expression nodes
	// executed on the line.
	Coverage float64 `json:"coverage"`
	// Nodes is the full set of expression nodes on
	// the line.
	Nodes []int64 `json:"nodes"`
	// Nodes is the set of expression nodes that were
	// executed.
	Covered []int64 `json:"covered"`
	// Nodes is the set of expression nodes that were
	// not executed.
	Missed []int64 `json:"missed"`
	// Annotation is a textual representation of the
	// line, marking positions that were not executed.
	Annotation string `json:"annotation"`
}

func (c LineCoverage) String() string {
	if c.Annotation == "" {
		return fmt.Sprintf("%d: %0.2f (%d/%d)", c.Line, c.Coverage, len(c.Covered), len(c.Nodes))
	}
	return fmt.Sprintf("%d: %0.2f (%d/%d) %v\n%s", c.Line, c.Coverage, len(c.Covered), len(c.Nodes), c.Missed, c.Annotation)
}
