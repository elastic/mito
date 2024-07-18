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
	"io"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"

	"github.com/elastic/mito/lib/xml"
)

// XML returns a cel.EnvOption to configure extended functions for XML
// decoding. The parameter specifies the CEL type adapter to use and a
// map of names to XSD document descriptions.
// A nil adapter is valid and will give an option using the default type
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
func XML(adapter types.Adapter, xsd map[string]string) (cel.EnvOption, error) {
	if adapter == nil {
		adapter = types.DefaultTypeAdapter
	}
	details := make(map[string]map[string]xml.Detail, len(xsd))
	var err error
	for name, doc := range xsd {
		details[name], err = xml.Details([]byte(doc))
		if err != nil {
			return nil, err
		}
	}
	return cel.Lib(xmlLib{adapter: adapter, xsdDetails: details}), nil
}

type xmlLib struct {
	adapter types.Adapter

	xsdDetails map[string]map[string]xml.Detail
}

func (l xmlLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function("decode_xml",
			// Without type information.
			cel.MemberOverload(
				"string_decode_xml",
				[]*cel.Type{cel.StringType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeXML)),
			),
			cel.Overload(
				"decode_xml_string",
				[]*cel.Type{cel.StringType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeXML)),
			),
			cel.MemberOverload(
				"bytes_decode_xml",
				[]*cel.Type{cel.BytesType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeXML)),
			),
			cel.Overload(
				"decode_xml_bytes",
				[]*cel.Type{cel.BytesType},
				cel.DynType,
				cel.UnaryBinding(catch(l.decodeXML)),
			),

			// With type information.
			cel.MemberOverload(
				"string_decode_xml_string",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.DynType,
				cel.BinaryBinding(catch(l.decodeXMLWithXSD)),
			),
			cel.Overload(
				"decode_xml_string_string",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.DynType,
				cel.BinaryBinding(catch(l.decodeXMLWithXSD)),
			),
			cel.MemberOverload(
				"bytes_decode_xml_string",
				[]*cel.Type{cel.BytesType, cel.StringType},
				cel.DynType,
				cel.BinaryBinding(catch(l.decodeXMLWithXSD)),
			),
			cel.Overload(
				"decode_xml_bytes_string",
				[]*cel.Type{cel.BytesType, cel.StringType},
				cel.DynType,
				cel.BinaryBinding(catch(l.decodeXMLWithXSD)),
			),
		),
	}
}

func (xmlLib) ProgramOptions() []cel.ProgramOption { return nil }

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
	cdata, v, err := xml.Unmarshal(r, details)
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
