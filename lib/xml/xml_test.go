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

package xml

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

var pathDetailsTests = []struct {
	xsd  string
	want map[string]Detail
}{
	0: {
		xsd: `
<?xml version="1.0" encoding="UTF-8" standalone="no"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
<xs:element name="REPORT_LIST_OUTPUT">
<xs:complexType>
<xs:sequence>
<xs:element minOccurs="0" ref="REQUEST"/>
<xs:element ref="RESPONSE"/>
</xs:sequence>
</xs:complexType>
</xs:element>
<xs:element name="REQUEST">
<xs:complexType>
<xs:sequence>
<xs:element ref="DATETIME"/>
<xs:element ref="USER_LOGIN"/>
<xs:element ref="RESOURCE" type="xs:string"/>
<xs:element minOccurs="0" ref="PARAM_LIST"/>
<xs:element minOccurs="0" ref="POST_DATA"/>
</xs:sequence>
</xs:complexType>
</xs:element>
<xs:element name="DATETIME" type="xs:string"/>
<xs:element name="USER_LOGIN" type="xs:string"/>
<xs:element name="RESOURCE" type="xs:string"/>
<xs:element name="PARAM_LIST">
<xs:complexType>
<xs:sequence>
<xs:element maxOccurs="unbounded" ref="PARAM"/>
</xs:sequence>
</xs:complexType>
</xs:element>
<xs:element name="PARAM">
<xs:complexType>
<xs:sequence>
<xs:element ref="KEY"/>
<xs:element ref="VALUE"/>
</xs:sequence>
</xs:complexType>
</xs:element>
<xs:element name="KEY" type="xs:string"/>
<xs:element name="VALUE" type="xs:string"/>
<xs:element name="POST_DATA" type="xs:string"/>
<xs:element name="RESPONSE">
<xs:complexType>
<xs:sequence>
<xs:element ref="DATETIME"/>
<xs:element minOccurs="0" ref="REPORT_LIST"/>
</xs:sequence>
</xs:complexType>
</xs:element>
<xs:element name="REPORT_LIST">
<xs:complexType>
<xs:sequence>
<xs:element maxOccurs="unbounded" ref="REPORT"/>
</xs:sequence>
</xs:complexType>
</xs:element>
<xs:element name="REPORT">
<xs:complexType>
<xs:sequence>
<xs:element ref="ID"/>
<xs:element minOccurs="0" ref="TITLE"/>
<xs:element minOccurs="0" ref="CLIENT"/>
<xs:element ref="TYPE"/>
<xs:element ref="USER_LOGIN"/>
<xs:element ref="LAUNCH_DATETIME"/>
<xs:element ref="OUTPUT_FORMAT"/>
<xs:element ref="SIZE"/>
<xs:element ref="STATUS"/>
<xs:element ref="EXPIRATION_DATETIME"/>
</xs:sequence>
</xs:complexType>
</xs:element>
<xs:element name="ID" type="xs:string"/>
<xs:element name="TITLE" type="xs:string"/>
<xs:element name="CLIENT">
<xs:complexType>
<xs:sequence>
<xs:element ref="ID"/>
<xs:element ref="NAME"/>
</xs:sequence>
</xs:complexType>
</xs:element>
<xs:element name="NAME" type="xs:string"/>
<xs:element name="TYPE" type="xs:string"/>
<xs:element name="LAUNCH_DATETIME" type="xs:string"/>
<xs:element name="OUTPUT_FORMAT" type="xs:string"/>
<xs:element name="SIZE" type="xs:string"/>
<xs:element name="STATUS">
<xs:complexType>
<xs:sequence>
<xs:element ref="STATE"/>
<xs:element minOccurs="0" ref="MESSAGE"/>
<xs:element minOccurs="0" ref="PERCENT"/>
</xs:sequence>
</xs:complexType>
</xs:element>
<xs:element name="STATE" type="xs:string"/>
<xs:element name="MESSAGE" type="xs:string"/>
<xs:element name="PERCENT" type="xs:string"/>
<xs:element name="EXPIRATION_DATETIME" type="xs:string"/>
</xs:schema>
`,
		want: map[string]Detail{
			"REPORT_LIST_OUTPUT": {
				Children: map[string]Detail{
					"REQUEST": {
						Children: map[string]Detail{
							"PARAM_LIST": {
								Children: map[string]Detail{
									"PARAM": {
										Plural: true,
									},
								},
							},
						},
					},
					"RESPONSE": {
						Children: map[string]Detail{
							"REPORT_LIST": {
								Children: map[string]Detail{
									"REPORT": {
										Plural: true,
									},
								},
							},
						},
					},
				},
			},
		},
	},
	1: {
		xsd: `
<?xml version="1.0" encoding="UTF-8" ?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="order">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="sender" type="xs:string"/>
        <xs:element name="address">
          <xs:complexType>
            <xs:sequence>
              <xs:element name="name" type="xs:string"/>
              <xs:element name="company" type="xs:string"/>
              <xs:element name="address" type="xs:string"/>
              <xs:element name="city" type="xs:string"/>
              <xs:element name="country" type="xs:string"/>
            </xs:sequence>
          </xs:complexType>
        </xs:element>
        <xs:element name="item" maxOccurs="unbounded">
          <xs:complexType>
            <xs:sequence>
              <xs:element name="name" type="xs:string"/>
              <xs:element name="note" type="xs:string" minOccurs="0"/>
              <xs:element name="number" type="xs:positiveInteger"/>
              <xs:element name="cost" type="xs:decimal"/>
              <xs:element name="sent" type="xs:boolean"/>
            </xs:sequence>
          </xs:complexType>
        </xs:element>
      </xs:sequence>
      <xs:attribute name="orderid" type="xs:string" use="required"/>
    </xs:complexType>
  </xs:element>
</xs:schema>
`,
		want: map[string]Detail{
			"order": {
				Children: map[string]Detail{
					"item": {
						Plural: true,
						Children: map[string]Detail{
							"cost": {
								Type: FloatType,
							},
							"number": {
								Type: IntType,
							},
							"sent": {
								Type: BoolType,
							},
						},
					},
				},
			},
		},
	},
}

func TestPathDetails(t *testing.T) {
	for _, test := range pathDetailsTests {
		t.Run("", func(t *testing.T) {
			got, err := Details([]byte(test.xsd))
			if err != nil {
				t.Errorf("failed to get path details: %v", err)
			}
			allow := cmp.AllowUnexported(Detail{})
			if !cmp.Equal(got, test.want, allow) {
				t.Errorf("unexpected result:\n--- want\n+++ got\n%s", cmp.Diff(test.want, got, allow))
			}
		})
	}
}

var decodeXMLTests = []struct {
	doc       string
	xsd       string
	wantCDATA string
	wantElems map[string]any
}{
	0: {
		doc: `
<?xml version="1.0" encoding="UTF-8" ?>
<!DOCTYPE REPORT_LIST_OUTPUT SYSTEM "https://qualysapi.qualys.com/api/2.0/fo/report/report_list_output.dtd">
<!--sample comment-->
<REPORT_LIST_OUTPUT>
	<RESPONSE>
		<DATETIME>2017-10-30T22:32:15Z</DATETIME>
		<REPORT_LIST>
			<REPORT>
				<ID>42703</ID>
				<TITLE>
					<![CDATA[Test now]]>
				</TITLE>
				<TYPE>Scan</TYPE>
				<USER_LOGIN>acme_aa</USER_LOGIN>
				<LAUNCH_DATETIME>2017-10-30T17:59:22Z</LAUNCH_DATETIME>
				<OUTPUT_FORMAT>PDF</OUTPUT_FORMAT>
				<SIZE>129.1 MB</SIZE>
				<STATUS>
					<STATE>Finished</STATE>
				</STATUS>
				<EXPIRATION_DATETIME>2017-11-06T17:59:24Z</EXPIRATION_DATETIME>
			</REPORT>
		</REPORT_LIST>
	</RESPONSE>
</REPORT_LIST_OUTPUT>
`,
		xsd: `
<?xml version="1.0" encoding="UTF-8" standalone="no"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
<xs:element name="REPORT_LIST_OUTPUT">
<xs:complexType>
<xs:sequence>
<xs:element minOccurs="0" ref="REQUEST"/>
<xs:element ref="RESPONSE"/>
</xs:sequence>
</xs:complexType>
</xs:element>
<xs:element name="REQUEST">
<xs:complexType>
<xs:sequence>
<xs:element ref="DATETIME"/>
<xs:element ref="USER_LOGIN"/>
<xs:element ref="RESOURCE" type="xs:string"/>
<xs:element minOccurs="0" ref="PARAM_LIST"/>
<xs:element minOccurs="0" ref="POST_DATA"/>
</xs:sequence>
</xs:complexType>
</xs:element>
<xs:element name="DATETIME" type="xs:string"/>
<xs:element name="USER_LOGIN" type="xs:string"/>
<xs:element name="RESOURCE" type="xs:string"/>
<xs:element name="PARAM_LIST">
<xs:complexType>
<xs:sequence>
<xs:element maxOccurs="unbounded" ref="PARAM"/>
</xs:sequence>
</xs:complexType>
</xs:element>
<xs:element name="PARAM">
<xs:complexType>
<xs:sequence>
<xs:element ref="KEY"/>
<xs:element ref="VALUE"/>
</xs:sequence>
</xs:complexType>
</xs:element>
<xs:element name="KEY" type="xs:string"/>
<xs:element name="VALUE" type="xs:string"/>
<xs:element name="POST_DATA" type="xs:string"/>
<xs:element name="RESPONSE">
<xs:complexType>
<xs:sequence>
<xs:element ref="DATETIME"/>
<xs:element minOccurs="0" ref="REPORT_LIST"/>
</xs:sequence>
</xs:complexType>
</xs:element>
<xs:element name="REPORT_LIST">
<xs:complexType>
<xs:sequence>
<xs:element maxOccurs="unbounded" ref="REPORT"/>
</xs:sequence>
</xs:complexType>
</xs:element>
<xs:element name="REPORT">
<xs:complexType>
<xs:sequence>
<xs:element ref="ID"/>
<xs:element minOccurs="0" ref="TITLE"/>
<xs:element minOccurs="0" ref="CLIENT"/>
<xs:element ref="TYPE"/>
<xs:element ref="USER_LOGIN"/>
<xs:element ref="LAUNCH_DATETIME"/>
<xs:element ref="OUTPUT_FORMAT"/>
<xs:element ref="SIZE"/>
<xs:element ref="STATUS"/>
<xs:element ref="EXPIRATION_DATETIME"/>
</xs:sequence>
</xs:complexType>
</xs:element>
<xs:element name="ID" type="xs:string"/>
<xs:element name="TITLE" type="xs:string"/>
<xs:element name="CLIENT">
<xs:complexType>
<xs:sequence>
<xs:element ref="ID"/>
<xs:element ref="NAME"/>
</xs:sequence>
</xs:complexType>
</xs:element>
<xs:element name="NAME" type="xs:string"/>
<xs:element name="TYPE" type="xs:string"/>
<xs:element name="LAUNCH_DATETIME" type="xs:string"/>
<xs:element name="OUTPUT_FORMAT" type="xs:string"/>
<xs:element name="SIZE" type="xs:string"/>
<xs:element name="STATUS">
<xs:complexType>
<xs:sequence>
<xs:element ref="STATE"/>
<xs:element minOccurs="0" ref="MESSAGE"/>
<xs:element minOccurs="0" ref="PERCENT"/>
</xs:sequence>
</xs:complexType>
</xs:element>
<xs:element name="STATE" type="xs:string"/>
<xs:element name="MESSAGE" type="xs:string"/>
<xs:element name="PERCENT" type="xs:string"/>
<xs:element name="EXPIRATION_DATETIME" type="xs:string"/>
</xs:schema>
`,
		wantCDATA: "",
		wantElems: map[string]any{
			"REPORT_LIST_OUTPUT": map[string]any{
				"RESPONSE": map[string]any{
					"DATETIME": "2017-10-30T22:32:15Z",
					"REPORT_LIST": map[string]any{
						"REPORT": []any{
							map[string]any{
								"EXPIRATION_DATETIME": "2017-11-06T17:59:24Z",
								"ID":                  "42703",
								"LAUNCH_DATETIME":     "2017-10-30T17:59:22Z",
								"OUTPUT_FORMAT":       "PDF",
								"SIZE":                "129.1 MB",
								"STATUS": map[string]any{
									"STATE": "Finished",
								},
								"TITLE":      "",
								"TYPE":       "Scan",
								"USER_LOGIN": "acme_aa",
							},
						},
					},
				},
			},
		},
	},
	1: {
		doc: `
<?xml version="1.0" encoding="UTF-8"?>
<order orderid="56733" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:noNamespaceSchemaLocation="sales.xsd">
  <sender>Ástríðr Ragnar</sender>
  <address>
    <name>Joord Lennart</name>
    <company>Sydøstlige Gruppe</company>
    <address>Beekplantsoen 594, 2 hoog, 6849 IG</address>
    <city>Boekend</city>
    <country>Netherlands</country>
  </address>
  <item>
    <name>Egil's Saga</name>
    <note>Free Sample</note>
    <number>1</number>
    <cost>99.95</cost>
    <sent>FALSE</sent>
  </item>
  <item>
    <name>Auðunar þáttr vestfirska</name>
    <number>1</number>
    <cost>9.90</cost>
    <sent>TRUE</sent>
  </item>
</order>
`,
		xsd: `
<?xml version="1.0" encoding="UTF-8" ?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="order">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="sender" type="xs:string"/>
        <xs:element name="address">
          <xs:complexType>
            <xs:sequence>
              <xs:element name="name" type="xs:string"/>
              <xs:element name="company" type="xs:string"/>
              <xs:element name="address" type="xs:string"/>
              <xs:element name="city" type="xs:string"/>
              <xs:element name="country" type="xs:string"/>
            </xs:sequence>
          </xs:complexType>
        </xs:element>
        <xs:element name="item" maxOccurs="unbounded">
          <xs:complexType>
            <xs:sequence>
              <xs:element name="name" type="xs:string"/>
              <xs:element name="note" type="xs:string" minOccurs="0"/>
              <xs:element name="number" type="xs:positiveInteger"/>
              <xs:element name="cost" type="xs:decimal"/>
              <xs:element name="sent" type="xs:boolean"/>
            </xs:sequence>
          </xs:complexType>
        </xs:element>
      </xs:sequence>
      <xs:attribute name="orderid" type="xs:string" use="required"/>
    </xs:complexType>
  </xs:element>
</xs:schema>
`,
		wantCDATA: "",
		wantElems: map[string]any{
			"order": map[string]any{
				"address": map[string]any{
					"address": "Beekplantsoen 594, 2 hoog, 6849 IG",
					"city":    "Boekend",
					"company": "Sydøstlige Gruppe",
					"country": "Netherlands",
					"name":    "Joord Lennart",
				},
				"item": []any{
					map[string]any{
						"cost":   99.95,
						"name":   "Egil's Saga",
						"note":   "Free Sample",
						"number": int64(1),
						"sent":   false,
					},
					map[string]any{
						"cost":   9.9,
						"name":   "Auðunar þáttr vestfirska",
						"number": int64(1),
						"sent":   true,
					},
				},
				"noNamespaceSchemaLocation": "sales.xsd",
				"orderid":                   "56733",
				"sender":                    "Ástríðr Ragnar",
				"xsi":                       "http://www.w3.org/2001/XMLSchema-instance",
			},
		},
	},
}

func TestDecodeXML(t *testing.T) {
	for _, test := range decodeXMLTests {
		t.Run("", func(t *testing.T) {
			det, err := Details([]byte(test.xsd))
			if err != nil {
				t.Fatalf("failed to get path details: %v", err)
			}
			gotCDATA, gotElems, err := Unmarshal(strings.NewReader(test.doc), det)
			if err != nil {
				t.Errorf("failed to decode doc: %v", err)
			}
			if gotCDATA != test.wantCDATA {
				t.Errorf("unexpected CDATA:\ngot: %s\nwant:%s", gotCDATA, test.wantCDATA)
			}
			if !cmp.Equal(gotElems, test.wantElems) {
				t.Errorf("unexpected result\n--- want\n+++ got\n%s", cmp.Diff(test.wantElems, gotElems))
			}
		})
	}
}
