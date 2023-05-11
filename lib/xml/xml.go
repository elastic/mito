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

// Package xml provides an XSD-based dynamically typed xml decoder.
package xml

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"aqwari.net/xml/xsd"
)

// Detail is a type and plurality Detail node in a XSD tree.
type Detail struct {
	// Type is the type of the tree node.
	Type Type
	// Plural is whether the node is a list.
	Plural bool

	// Children are the node's children.
	Children map[string]Detail
}

func (d Detail) isZero() bool {
	return d.Type == 0 && !d.Plural && d.Children == nil
}

// Type is an enriched JSON type system reflecting Go's treatment of it for numbers.
type Type int

const (
	StringType = iota
	IntType
	FloatType
	BoolType
)

// Details returns type and plurality details obtained from the provided XSD doc. Only
// interesting nodes are retained in the type hint tree. Interesting nodes are either
// plural, integer, float or bool, or have children at some depth that are plural, integer
// float or bool.
func Details(doc []byte) (map[string]Detail, error) {
	schema, err := xsd.Parse(doc)
	if err != nil {
		return nil, err
	}

	tree := make(map[string]string)
	leaves := make(map[string]Detail)
	for _, s := range schema {
		for n, t := range s.Types {
			switch t := t.(type) {
			case xsd.Builtin:
			case *xsd.SimpleType:
			case *xsd.ComplexType:
				// Ignore external name-spaced names.
				if t.Name.Space != "" {
					continue
				}
				// Ignore anonymous node and the root.
				if strings.HasPrefix(n.Local, "_") {
					continue
				}
				for _, e := range t.Elements {
					var d Detail
					tree[e.Name.Local] = n.Local
					switch builtinTypeFor(e.Type) {
					case xsd.Boolean:
						d.Type = BoolType
					case xsd.Int, xsd.Integer, xsd.Long, xsd.NonNegativeInteger,
						xsd.NonPositiveInteger, xsd.PositiveInteger, xsd.Short,
						xsd.UnsignedByte, xsd.UnsignedInt, xsd.UnsignedLong, xsd.UnsignedShort:
						d.Type = IntType
					case xsd.Decimal, xsd.Double, xsd.Float:
						d.Type = FloatType
					}
					d.Plural = e.Plural
					if d.isZero() {
						continue
					}
					leaves[e.Name.Local] = d
				}
			default:
				panic(fmt.Sprintf("unknown type: %T", t))
			}
		}
	}

	details := Detail{Children: make(map[string]Detail)}
	var path []string
	for p, d := range leaves {
		path = append(path[:0], p)
		for i := 0; i <= len(tree); i++ {
			parent, ok := tree[p]
			if !ok {
				break
			}
			path = append(path, parent)
			p = parent
		}
		reverse(path)

		n := details
		for i, e := range path {
			c := n.Children[e]
			if c.Children == nil && i < len(path)-1 {
				c.Children = make(map[string]Detail)
			}
			if i == len(path)-1 {
				c.Type = d.Type
				c.Plural = d.Plural
			}
			n.Children[e] = c
			n = c
		}
	}

	return details.Children, nil
}

func reverse(s []string) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// builtinTypeFor returns the built-in type for the type if available. Otherwise it returns xsd.Anytype.
func builtinTypeFor(typ xsd.Type) xsd.Builtin {
	for {
		switch t := typ.(type) {
		case xsd.Builtin:
			return t
		case *xsd.SimpleType:
			typ = xsd.Base(t.Base)
		default:
			return xsd.AnyType
		}
	}
}

// Unmarshal decodes the data in r using type and plurality hints in details. If details is
// nil, best effort plurality assessment will be made and all data will be represented as
// strings.
func Unmarshal(r io.Reader, details map[string]Detail) (cdata string, elems map[string]any, err error) {
	dec := xml.NewDecoder(r)
	dec.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) { return input, nil }
	var w walker
	cdata, elems, err = w.walkXML(dec, nil, details)
	if err == nil && !w.wasValidXML() {
		err = io.ErrUnexpectedEOF
	}
	return cdata, elems, err
}

type walker struct {
	hasDecl bool
	hasElem bool
}

func (w *walker) wasValidXML() bool {
	return w.hasDecl && w.hasElem
}

func (w *walker) walkXML(dec *xml.Decoder, attrs []xml.Attr, details map[string]Detail) (cdata string, elems map[string]any, err error) {
	elems = map[string]any{}

	for {
		t, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return "", elems, nil
			}
			return "", nil, err
		}

		switch elem := t.(type) {
		case xml.ProcInst:
			if w.hasDecl {
				continue
			}
			w.hasDecl = elem.Target == "xml"

		case xml.StartElement:
			key := elem.Name.Local
			det := details[key]
			w.hasElem = true

			var part map[string]any
			cdata, part, err = w.walkXML(dec, elem.Attr, det.Children)
			if err != nil {
				return "", nil, err
			}

			// Combine sub-elements and cdata.
			var add any = part
			if len(part) == 0 {
				add = cdata
			} else if len(cdata) != 0 {
				part["#text"] = cdata
			}

			// Add the data to the current object while taking into account
			// if the current key already exists (in the case of lists).
			value := elems[key]
			switch v := value.(type) {
			case nil:
				add = entype(add, det.Type)
				if det.Plural {
					elems[key] = []any{add}
				} else {
					elems[key] = add
				}
			case []any:
				add = entype(add, det.Type)
				elems[key] = append(v, add)
			default:
				add = entype(add, det.Type)
				elems[key] = []any{v, add}
			}

		case xml.CharData:
			cdata = string(bytes.TrimSpace(elem.Copy()))

		case xml.EndElement:
			for _, attr := range attrs {
				elems[attr.Name.Local] = attr.Value
			}
			return cdata, elems, nil
		}
	}
}

// entype attempts to render the element value as the expected type, falling
// back to a string if it is not possible.
func entype(v any, t Type) any {
	switch v := v.(type) {
	case string:
		switch t {
		case BoolType:
			switch v {
			case "TRUE":
				return true
			case "FALSE":
				return false
			default:
				return v
			}
		case IntType:
			i, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return v
			}
			return i
		case FloatType:
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return v
			}
			return f
		default:
			return v
		}
	default:
		return v
	}
}
