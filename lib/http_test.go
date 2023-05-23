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
	"net/http"
	"testing"
)

// digestChallengeTests are test vectors from RFC2617 and RFC7616.
var digestChallengeTests = []struct {
	name       string
	header     string
	wantParsed digestChallenge

	method       string
	uri          string
	username     string
	password     string
	body         string
	cnonce       string
	wantResponse string
	wantHeader   string
}{
	{
		name: "rfc2617",
		header: `Digest ` +
			`realm="testrealm@host.com", ` +
			`qop="auth,auth-int", ` +
			`nonce="dcd98b7102dd2f0e8b11d0f600bfb0c093", ` +
			`opaque="5ccc069c403ebaf9f0171e9517f40e41"`,
		wantParsed: digestChallenge{
			realm:  "testrealm@host.com",
			qop:    "auth,auth-int",
			nonce:  "dcd98b7102dd2f0e8b11d0f600bfb0c093",
			opaque: "5ccc069c403ebaf9f0171e9517f40e41",
		},

		method:       http.MethodGet,
		uri:          "/dir/index.html",
		username:     "Mufasa",
		password:     "Circle Of Life",
		cnonce:       "0a4f113b",
		wantResponse: "6629fae49393a05397450978507c4ef1",
		wantHeader: `Digest ` +
			`response="6629fae49393a05397450978507c4ef1",` +
			`opaque="5ccc069c403ebaf9f0171e9517f40e41",` +
			`username="Mufasa",` +
			`realm="testrealm@host.com",` +
			`uri="/dir/index.html",` +
			`nonce="dcd98b7102dd2f0e8b11d0f600bfb0c093",` +
			`qop=auth,` +
			`cnonce="0a4f113b",` +
			`nc=00000001`,
	},
	{
		name: "rfc7616_256",
		header: `Digest ` +
			`realm="http-auth@example.org", ` +
			`qop="auth, ` +
			`auth-int", ` +
			`algorithm=SHA-256, ` +
			`nonce="7ypf/xlj9XXwfDPEoM4URrv/xwf94BcCAzFZH4GiTo0v", ` +
			`opaque="FQhe/qaU925kfnzjCev0ciny7QMkPqMAFRtzCUYo5tdS"`,
		wantParsed: digestChallenge{
			realm:     "http-auth@example.org",
			qop:       "auth, auth-int",
			algorithm: "SHA-256",
			nonce:     "7ypf/xlj9XXwfDPEoM4URrv/xwf94BcCAzFZH4GiTo0v",
			opaque:    "FQhe/qaU925kfnzjCev0ciny7QMkPqMAFRtzCUYo5tdS",
		},

		method:       http.MethodGet,
		uri:          "/dir/index.html",
		username:     "Mufasa",
		password:     "Circle of Life",
		cnonce:       "f2/wE4q74E6zIJEtWaHKaf5wv/H5QzzpXusqGemxURZJ",
		wantResponse: "753927fa0e85d155564e2e272a28d1802ca10daf4496794697cf8db5856cb6c1",
		wantHeader: `Digest ` +
			`response="753927fa0e85d155564e2e272a28d1802ca10daf4496794697cf8db5856cb6c1",` +
			`opaque="FQhe/qaU925kfnzjCev0ciny7QMkPqMAFRtzCUYo5tdS",` +
			`algorithm=SHA-256,` +
			`username="Mufasa",` +
			`realm="http-auth@example.org",` +
			`uri="/dir/index.html",` +
			`nonce="7ypf/xlj9XXwfDPEoM4URrv/xwf94BcCAzFZH4GiTo0v",` +
			`qop=auth,` +
			`cnonce="f2/wE4q74E6zIJEtWaHKaf5wv/H5QzzpXusqGemxURZJ",` +
			`nc=00000001`,
	},
	{
		name: "rfc7616_MD5",
		header: `Digest ` +
			`realm="http-auth@example.org", ` +
			`qop="auth, ` +
			`auth-int", ` +
			`algorithm=MD5, ` +
			`nonce="7ypf/xlj9XXwfDPEoM4URrv/xwf94BcCAzFZH4GiTo0v", ` +
			`opaque="FQhe/qaU925kfnzjCev0ciny7QMkPqMAFRtzCUYo5tdS"`,
		wantParsed: digestChallenge{
			realm:     "http-auth@example.org",
			qop:       "auth, auth-int",
			algorithm: "MD5",
			nonce:     "7ypf/xlj9XXwfDPEoM4URrv/xwf94BcCAzFZH4GiTo0v",
			opaque:    "FQhe/qaU925kfnzjCev0ciny7QMkPqMAFRtzCUYo5tdS",
		},

		method:       http.MethodGet,
		uri:          "/dir/index.html",
		username:     "Mufasa",
		password:     "Circle of Life",
		cnonce:       "f2/wE4q74E6zIJEtWaHKaf5wv/H5QzzpXusqGemxURZJ",
		wantResponse: "8ca523f5e9506fed4657c9700eebdbec",
		wantHeader: `Digest ` +
			`response="8ca523f5e9506fed4657c9700eebdbec",` +
			`opaque="FQhe/qaU925kfnzjCev0ciny7QMkPqMAFRtzCUYo5tdS",` +
			`algorithm=MD5,` +
			`username="Mufasa",` +
			`realm="http-auth@example.org",` +
			`uri="/dir/index.html",` +
			`nonce="7ypf/xlj9XXwfDPEoM4URrv/xwf94BcCAzFZH4GiTo0v",` +
			`qop=auth,` +
			`cnonce="f2/wE4q74E6zIJEtWaHKaf5wv/H5QzzpXusqGemxURZJ",` +
			`nc=00000001`,
	},
	{
		name: "rfc7616_512_userhash",
		header: `Digest ` +
			`realm="api@example.org", ` +
			`qop="auth", ` +
			`algorithm=SHA-512-256, ` +
			`nonce="5TsQWLVdgBdmrQ0XsxbDODV+57QdFR34I9HAbC/RVvkK", ` +
			`opaque="HRPCssKJSGjCrkzDg8OhwpzCiGPChXYjwrI2QmXDnsOS", ` +
			`charset=UTF-8, ` +
			`userhash=true`,
		wantParsed: digestChallenge{
			realm:     "api@example.org",
			qop:       "auth",
			algorithm: "SHA-512-256",
			nonce:     "5TsQWLVdgBdmrQ0XsxbDODV+57QdFR34I9HAbC/RVvkK",
			opaque:    "HRPCssKJSGjCrkzDg8OhwpzCiGPChXYjwrI2QmXDnsOS",
			charset:   "UTF-8",
			userhash:  true,
		},

		method:       http.MethodGet,
		uri:          "/doe.json",
		username:     "Jäsøn Doe",
		password:     "Secret, or not?",
		cnonce:       "NTg6RKcb9boFIAS3KrFK9BGeh+iDa/sm6jUMp2wds69v",
		wantResponse: "ae66e67d6b427bd3f120414a82e4acff38e8ecd9101d6c861229025f607a79dd",
		wantHeader: `Digest ` +
			`response="ae66e67d6b427bd3f120414a82e4acff38e8ecd9101d6c861229025f607a79dd",` +
			`opaque="HRPCssKJSGjCrkzDg8OhwpzCiGPChXYjwrI2QmXDnsOS",` +
			`algorithm=SHA-512-256,` +
			`username="488869477bf257147b804c45308cd62ac4e25eb717b12b298c79e62dcea254ec",` +
			`realm="api@example.org",` +
			`uri="/doe.json",` +
			`nonce="5TsQWLVdgBdmrQ0XsxbDODV+57QdFR34I9HAbC/RVvkK",` +
			`qop=auth,` +
			`cnonce="NTg6RKcb9boFIAS3KrFK9BGeh+iDa/sm6jUMp2wds69v",` +
			`nc=00000001,` +
			`userhash=true`,
	},
	{
		name: "rfc7616_512_no_userhash",
		header: `Digest ` +
			`realm="api@example.org", ` +
			`qop="auth", ` +
			`algorithm=SHA-512-256, ` +
			`nonce="5TsQWLVdgBdmrQ0XsxbDODV+57QdFR34I9HAbC/RVvkK", ` +
			`opaque="HRPCssKJSGjCrkzDg8OhwpzCiGPChXYjwrI2QmXDnsOS", ` +
			`charset=UTF-8, ` +
			`userhash=false`,
		wantParsed: digestChallenge{
			realm:     "api@example.org",
			qop:       "auth",
			algorithm: "SHA-512-256",
			nonce:     "5TsQWLVdgBdmrQ0XsxbDODV+57QdFR34I9HAbC/RVvkK",
			opaque:    "HRPCssKJSGjCrkzDg8OhwpzCiGPChXYjwrI2QmXDnsOS",
			charset:   "UTF-8",
			userhash:  false,
		},

		method:       http.MethodGet,
		uri:          "/doe.json",
		username:     "Jäsøn Doe",
		password:     "Secret, or not?",
		cnonce:       "NTg6RKcb9boFIAS3KrFK9BGeh+iDa/sm6jUMp2wds69v",
		wantResponse: "ae66e67d6b427bd3f120414a82e4acff38e8ecd9101d6c861229025f607a79dd",
		wantHeader: `Digest ` +
			`response="ae66e67d6b427bd3f120414a82e4acff38e8ecd9101d6c861229025f607a79dd",` +
			`opaque="HRPCssKJSGjCrkzDg8OhwpzCiGPChXYjwrI2QmXDnsOS",` +
			`algorithm=SHA-512-256,` +
			`username*=UTF-8''J%C3%A4s%C3%B8n%20Doe,` +
			`realm="api@example.org",` +
			`uri="/doe.json",` +
			`nonce="5TsQWLVdgBdmrQ0XsxbDODV+57QdFR34I9HAbC/RVvkK",` +
			`qop=auth,` +
			`cnonce="NTg6RKcb9boFIAS3KrFK9BGeh+iDa/sm6jUMp2wds69v",` +
			`nc=00000001`,
	},
}

func TestParseDigestChallenge(t *testing.T) {
	for _, test := range digestChallengeTests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseDigestChallenge(test.header)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if *got != test.wantParsed {
				t.Errorf("unexpected result:\ngot: %#v\nwant:%#v", *got, test.wantParsed)
			}
		})
	}
}

func TestDigestChallenge(t *testing.T) {
	for _, test := range digestChallengeTests {
		if test.wantResponse == "" {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			header := make(http.Header)
			header.Add("www-authenticate", test.header)
			resp := &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     header,
				Request: &http.Request{
					RequestURI: test.uri,
					Method:     test.method,
				},
				Body: http.NoBody,
			}
			r, err := newDigestResponse(resp, test.username, test.password, test.cnonce, test.body, 0)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if r.response != test.wantResponse {
				t.Errorf("unexpected digest response hash: got:%s want:%s", r.response, test.wantResponse)
			}
			digestHeader := r.String()
			if digestHeader != test.wantHeader {
				t.Errorf("unexpected digest response header: got:%s want:%s", digestHeader, test.wantHeader)
			}
		})
	}
}
