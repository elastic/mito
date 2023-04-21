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
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"aqwari.net/xml/xsd"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter/functions"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// XML returns a cel.EnvOption to configure extended functions for XML
// decoding. The parameter specifies the CEL type adapter to use and a
// map of names to XSD document descriptions.
// A nil adapter is valid an will give an option using the default type
// adapter, types.DefaultTypeAdapter. A nil XSD mapping is valid and
// will give an option that performs best effort decoding leaving all
// values as strings and elevating elements to lists when more than one
// item is found for the path.
//
// # Decode XML
//
// decode_xml returns the object described by the XML encoding of the receiver
// or parameter, using an optional named XSD:
//
//	<bytes>.decode_xml() -> <dyn>
//	<string>.decode_xml() -> <dyn>
//	decode_xml(<bytes>) -> <dyn>
//	decode_xml(<string>) -> <dyn>
//	<bytes>.decode_xml(<string>) -> <dyn>
//	<string>.decode_xml(<string>) -> <dyn>
//	decode_xml(<bytes>, <string>) -> <dyn>
//	decode_xml(<string>, <string>) -> <dyn>
//
// Examples:
//
//	"<?xml vers... ...>".decode_xml()   // return { ... }
//	b"<?xml vers... ...>".decode_xml()   // return { ... }
//	"<?xml vers... ...>".decode_xml("xsd")   // return { ... }
//	b"<?xml vers... ...>".decode_xml("xsd")   // return { ... }
func XML(adapter ref.TypeAdapter, xsd map[string]string) (cel.EnvOption, error) {
	if adapter == nil {
		adapter = types.DefaultTypeAdapter
	}
	details := make(map[string]map[string]detail, len(xsd))
	var err error
	for name, doc := range xsd {
		details[name], err = pathDetails([]byte(doc))
		if err != nil {
			return nil, err
		}
	}
	return cel.Lib(xmlLib{adapter: adapter, xsdDetails: details}), nil
}

type xmlLib struct {
	adapter ref.TypeAdapter

	xsdDetails map[string]map[string]detail
}

func (xmlLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Declarations(
			decls.NewFunction("decode_xml",
				decls.NewOverload(
					"decode_xml_string_or_bytes",
					[]*expr.Type{decls.String},
					decls.Dyn,
				),
				decls.NewInstanceOverload(
					"string_or_bytes_decode_xml",
					[]*expr.Type{decls.String},
					decls.Dyn,
				),
				decls.NewOverload(
					"decode_xml_string_or_bytes",
					[]*expr.Type{decls.Bytes},
					decls.Dyn,
				),
				decls.NewInstanceOverload(
					"string_or_bytes_decode_xml",
					[]*expr.Type{decls.Bytes},
					decls.Dyn,
				),
				decls.NewOverload(
					"decode_xml_string_or_bytes_string",
					[]*expr.Type{decls.String, decls.String},
					decls.Dyn,
				),
				decls.NewInstanceOverload(
					"string_or_bytes_decode_xml_string",
					[]*expr.Type{decls.String, decls.String},
					decls.Dyn,
				),
				decls.NewOverload(
					"decode_xml_string_or_bytes_string",
					[]*expr.Type{decls.Bytes, decls.String},
					decls.Dyn,
				),
				decls.NewInstanceOverload(
					"string_or_bytes_decode_xml_string",
					[]*expr.Type{decls.Bytes, decls.String},
					decls.Dyn,
				),
			),
		),
	}
}

func (l xmlLib) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{
		cel.Functions(
			&functions.Overload{
				Operator: "decode_xml_string_or_bytes",
				Unary:    l.decodeXML,
			},
			&functions.Overload{
				Operator: "string_or_bytes_decode_xml",
				Unary:    l.decodeXML,
			},
			&functions.Overload{
				Operator: "decode_xml_string_or_bytes_string",
				Binary:   l.decodeXMLWithXSD,
			},
			&functions.Overload{
				Operator: "string_or_bytes_decode_xml_string",
				Binary:   l.decodeXMLWithXSD,
			},
		),
	}
}

func (l xmlLib) decodeXML(arg ref.Val) ref.Val {
	return l.decodeXMLWithXSD(arg, types.String(""))
}

func (l xmlLib) decodeXMLWithXSD(arg0, arg1 ref.Val) ref.Val {
	xsd, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(xsd, "no such overload for decode_xml: %s", arg1.Type())
	}
	details, ok := l.xsdDetails[string(xsd)]
	if !ok && xsd != "" {
		return types.NewErr("no xsd %s", xsd)
	}

	var r io.Reader
	switch msg := arg0.(type) {
	case types.Bytes:
		r = bytes.NewReader(msg)
	case types.String:
		r = strings.NewReader(string(msg))
	default:
		return types.NoSuchOverloadErr()
	}
	cdata, v, err := unmarshal(r, details)
	if err != nil {
		return types.NewErr("failed to unmarshal XML document: %v", err)
	}
	m := make(map[string]any)
	if cdata != "" {
		m["#text"] = cdata
	}
	if v != nil {
		m["doc"] = v
	}
	return l.adapter.NativeToValue(m)
}

// detail is a type and plurality detail node in a XSD tree.
type detail struct {
	typ    typ
	plural bool

	children map[string]detail
}

func (d detail) isZero() bool {
	return d.typ == 0 && !d.plural && d.children == nil
}

// typ is an enriched JSON type system reflecting Go's treatment of it for numbers.
type typ int

const (
	stringType = iota
	intType
	floatType
	boolType
)

// pathDetails returns type and plurality details obtained from the provided XSD doc.
func pathDetails(doc []byte) (map[string]detail, error) {
	schema, err := xsd.Parse(doc)
	if err != nil {
		return nil, err
	}

	tree := make(map[string]string)
	leaves := make(map[string]detail)
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
					var d detail
					tree[e.Name.Local] = n.Local
					switch builtinTypeFor(e.Type) {
					case xsd.Boolean:
						d.typ = boolType
					case xsd.Int, xsd.Integer, xsd.Long, xsd.NonNegativeInteger,
						xsd.NonPositiveInteger, xsd.PositiveInteger, xsd.Short,
						xsd.UnsignedByte, xsd.UnsignedInt, xsd.UnsignedLong, xsd.UnsignedShort:
						d.typ = intType
					case xsd.Decimal, xsd.Double, xsd.Float:
						d.typ = floatType
					}
					d.plural = e.Plural
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

	details := detail{children: make(map[string]detail)}
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
			c := n.children[e]
			if c.children == nil && i < len(path)-1 {
				c.children = make(map[string]detail)
			}
			if i == len(path)-1 {
				c.typ = d.typ
				c.plural = d.plural
			}
			n.children[e] = c
			n = c
		}
	}

	return details.children, nil
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

// unmarshal decodes the data in r using type and plurality hints in details. If details is
// nil, best effort plurality assessment will be made and all data will be represented as
// strings.
func unmarshal(r io.Reader, details map[string]detail) (cdata string, elems map[string]any, err error) {
	dec := xml.NewDecoder(r)
	dec.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) { return input, nil }
	return walkXML(dec, nil, details)
}

func walkXML(dec *xml.Decoder, attrs []xml.Attr, details map[string]detail) (cdata string, elems map[string]any, err error) {
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
		case xml.StartElement:
			key := elem.Name.Local
			det := details[key]

			var part map[string]any
			cdata, part, err = walkXML(dec, elem.Attr, det.children)
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
				add = entype(add, det.typ)
				if det.plural {
					elems[key] = []any{add}
				} else {
					elems[key] = add
				}
			case []any:
				add = entype(add, det.typ)
				elems[key] = append(v, add)
			default:
				add = entype(add, det.typ)
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
func entype(v any, t typ) any {
	switch v := v.(type) {
	case string:
		switch t {
		case boolType:
			switch v {
			case "TRUE":
				return true
			case "FALSE":
				return false
			default:
				return v
			}
		case intType:
			i, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return v
			}
			return i
		case floatType:
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
