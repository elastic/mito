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
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/google/cel-go/interpreter/functions"
	"golang.org/x/time/rate"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// HTTP returns a cel.EnvOption to configure extended functions for HTTP
// requests. Requests and responses are returned as maps corresponding to
// the Go http.Request and http.Response structs. The client and limit parameters
// will be used for the requests and API rate limiting. If client is nil
// the http.DefaultClient will be used and if limit is nil an non-limiting
// rate.Limiter will be used. If auth is not nil, the Authorization header
// is populated for Basic Authentication in requests constructed for direct
// HEAD, GET and POST method calls. Explicitly constructed requests used in
// do_request are not affected by auth. In cases where Basic Authentication
// is needed for these constructed requests, the basic_authentication method
// can be used to add the necessary header.
//
// # HEAD
//
// head performs a HEAD method request and returns the result:
//
//	head(<string>) -> <map<string,dyn>>
//
// Example:
//
//	head('http://www.example.com/')  // returns {"Body": "", "Close": false,
//
// # GET
//
// get performs a GET method request and returns the result:
//
//	get(<string>) -> <map<string,dyn>>
//
// Example:
//
//	get('http://www.example.com/')  // returns {"Body": "PCFkb2N0e...
//
// # GET Request
//
// get_request returns a GET method request:
//
//	get_request(<string>) -> <map<string,dyn>>
//
// Example:
//
//	get_request('http://www.example.com/')
//
//	will return:
//
//	{
//	    "Close": false,
//	    "ContentLength": 0,
//	    "Header": {},
//	    "Host": "www.example.com",
//	    "Method": "GET",
//	    "Proto": "HTTP/1.1",
//	    "ProtoMajor": 1,
//	    "ProtoMinor": 1,
//	    "URL": "http://www.example.com/"
//	}
//
// # POST
//
// post performs a POST method request and returns the result:
//
//	post(<string>, <string>, <bytes>) -> <map<string,dyn>>
//	post(<string>, <string>, <string>) -> <map<string,dyn>>
//
// Example:
//
//	post("http://www.example.com/", "text/plain", "test")  // returns {"Body": "PCFkb2N0e...
//
// # POST Request
//
// post_request returns a POST method request:
//
//	post_request(<string>, <string>, <bytes>) -> <map<string,dyn>>
//	post_request(<string>, <string>, <string>) -> <map<string,dyn>>
//
// Example:
//
//	post_request("http://www.example.com/", "text/plain", "test")
//
//	will return:
//
//	{
//	    "Body": "test",
//	    "Close": false,
//	    "ContentLength": 4,
//	    "Header": {
//	        "Content-Type": [
//	            "text/plain"
//	        ]
//	    },
//	    "Host": "www.example.com",
//	    "Method": "POST",
//	    "Proto": "HTTP/1.1",
//	    "ProtoMajor": 1,
//	    "ProtoMinor": 1,
//	    "URL": "http://www.example.com/"
//	}
//
// # Request
//
// request returns a user-defined method request:
//
//	request(<string>, <string>, <string>, <bytes>) -> <map<string,dyn>>
//	request(<string>, <string>, <string>, <string>) -> <map<string,dyn>>
//
// Example:
//
//	request("GET", "http://www.example.com/").with({"Header":{
//	    "Authorization": ["Basic "+string(base64("username:password"))],
//	}})
//
//	will return:
//
//	{
//	    "Close": false,
//	    "ContentLength": 0,
//	    "Header": {
//	        "Authorization": [
//	            "Basic dXNlcm5hbWU6cGFzc3dvcmQ="
//	        ]
//	    },
//	    "Host": "www.example.com",
//	    "Method": "GET",
//	    "Proto": "HTTP/1.1",
//	    "ProtoMajor": 1,
//	    "ProtoMinor": 1,
//	    "URL": "http://www.example.com/"
//	}
//
// # Basic Authentication
//
// basic_authentication adds a Basic Authentication Authorization header to a request,
// returning the modified request.
//
//	<map<string,dyn>>.basic_authentication(<string>, <string>) -> <map<string,dyn>>
//
// Example:
//
//	request("GET", "http://www.example.com/").basic_authentication("username", "password")
//
//	will return:
//
//	{
//	    "Close": false,
//	    "ContentLength": 0,
//	    "Header": {
//	        "Authorization": [
//	            "Basic dXNlcm5hbWU6cGFzc3dvcmQ="
//	        ]
//	    },
//	    "Host": "www.example.com",
//	    "Method": "GET",
//	    "Proto": "HTTP/1.1",
//	    "ProtoMajor": 1,
//	    "ProtoMinor": 1,
//	    "URL": "http://www.example.com/"
//	}
//
// # Do Request
//
// do_request executes an HTTP request:
//
//	<map<string,dyn>>.do_request() -> <map<string,dyn>>
//
// Example:
//
//	get_request("http://www.example.com/").do_request()  // returns {"Body": "PCFkb2N0e...
//
// # Parse URL
//
// parse_url returns a map holding the details of the parsed URL corresponding
// to the Go url.URL struct:
//
//	<string>.parse_url() -> <map<string,dyn>>
//
// Example:
//
//	"https://pkg.go.dev/net/url#URL".parse_url()
//
//	will return:
//
//	{
//	    "ForceQuery": false,
//	    "Fragment": "URL",
//	    "Host": "pkg.go.dev",
//	    "Opaque": "",
//	    "Path": "/net/url",
//	    "RawFragment": "",
//	    "RawPath": "",
//	    "RawQuery": "",
//	    "Scheme": "https",
//	    "User": null
//	}
//
// # Format URL
//
// format_url returns string corresponding to the URL map that is the receiver:
//
//	<map<string,dyn>>.format_url() -> <string>
//
// Example:
//
//	"https://pkg.go.dev/net/url#URL".parse_url().with_replace({"Host": "godoc.org"}).format_url()
//
//	will return:
//
//	"https://godoc.org/net/url#URL"
//
// # Parse Query
//
// parse_query returns a map holding the details of the parsed query corresponding
// to the Go url.Values map:
//
//	<string>.parse_query() -> <map<string,<list<string>>>
//
// Example:
//
//	"page=1&line=25".parse_url()
//
//	will return:
//
//	{
//	    "line": ["25"],
//	    "page": ["1"]
//	}
//
// # Format Query
//
// format_query returns string corresponding to the query map that is the receiver:
//
//	<map<string,<list<string>>>.format_query() -> <string>
//
// Example:
//
//	"page=1&line=25".parse_query().with_replace({"page":[string(2)]}).format_query()
//
//	will return:
//
//	line=25&page=2"
func HTTP(client *http.Client, limit *rate.Limiter, auth *BasicAuth) cel.EnvOption {
	return HTTPWithContext(context.Background(), client, limit, auth)
}

// HTTPWithContext returns a cel.EnvOption to configure extended functions
// for HTTP requests that include a context.Context in network requests.
func HTTPWithContext(ctx context.Context, client *http.Client, limit *rate.Limiter, auth *BasicAuth) cel.EnvOption {
	if client == nil {
		client = http.DefaultClient
	}
	if limit == nil {
		limit = rate.NewLimiter(rate.Inf, 0)
	}
	return cel.Lib(httpLib{
		client: client,
		limit:  limit,
		auth:   auth,
		ctx:    ctx,
	})
}

type httpLib struct {
	client *http.Client
	limit  *rate.Limiter
	auth   *BasicAuth
	ctx    context.Context
}

// BasicAuth is used to populate the Authorization header to use HTTP
// Basic Authentication with the provided username and password for
// direct HTTP method calls.
type BasicAuth struct {
	Username, Password string
}

func (httpLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Declarations(
			decls.NewFunction("head",
				decls.NewOverload(
					"head_string",
					[]*expr.Type{decls.String},
					decls.NewMapType(decls.String, decls.Dyn),
				),
			),
			decls.NewFunction("get",
				decls.NewOverload(
					"get_string",
					[]*expr.Type{decls.String},
					decls.NewMapType(decls.String, decls.Dyn),
				),
			),
			decls.NewFunction("get_request",
				decls.NewOverload(
					"get_request_string",
					[]*expr.Type{decls.String},
					decls.NewMapType(decls.String, decls.Dyn),
				),
			),
			decls.NewFunction("post",
				decls.NewOverload(
					"post_string_string_bytes",
					[]*expr.Type{decls.String, decls.String, decls.Bytes},
					decls.NewMapType(decls.String, decls.Dyn),
				),
				decls.NewOverload(
					"post_string_string_string",
					[]*expr.Type{decls.String, decls.String, decls.String},
					decls.NewMapType(decls.String, decls.Dyn),
				),
			),
			decls.NewFunction("post_request",
				decls.NewOverload(
					"post_request_string_string_bytes",
					[]*expr.Type{decls.String, decls.String, decls.Bytes},
					decls.NewMapType(decls.String, decls.Dyn),
				),
				decls.NewOverload(
					"post_request_string_string_string",
					[]*expr.Type{decls.String, decls.String, decls.String},
					decls.NewMapType(decls.String, decls.Dyn),
				),
			),
			decls.NewFunction("request",
				decls.NewOverload(
					"request_string_string",
					[]*expr.Type{decls.String, decls.String},
					decls.NewMapType(decls.String, decls.Dyn),
				),
				decls.NewOverload(
					"request_string_string_bytes",
					[]*expr.Type{decls.String, decls.String, decls.Bytes},
					decls.NewMapType(decls.String, decls.Dyn),
				),
				decls.NewOverload(
					"request_string_string_string",
					[]*expr.Type{decls.String, decls.String, decls.String},
					decls.NewMapType(decls.String, decls.Dyn),
				),
			),
			decls.NewFunction("basic_authentication",
				decls.NewInstanceOverload(
					"map_basic_authentication_string_string",
					[]*expr.Type{decls.NewMapType(decls.String, decls.Dyn), decls.String, decls.String},
					decls.NewMapType(decls.String, decls.Dyn),
				),
			),
			decls.NewFunction("do_request",
				decls.NewInstanceOverload(
					"map_do_request",
					[]*expr.Type{decls.NewMapType(decls.String, decls.Dyn)},
					decls.NewMapType(decls.String, decls.Dyn),
				),
			),
			decls.NewFunction("parse_url",
				decls.NewInstanceOverload(
					"string_parse_url",
					[]*expr.Type{decls.String},
					decls.NewMapType(decls.String, decls.Dyn),
				),
			),
			decls.NewFunction("format_url",
				decls.NewInstanceOverload(
					"map_format_url",
					[]*expr.Type{decls.NewMapType(decls.String, decls.Dyn)},
					decls.String,
				),
			),
			decls.NewFunction("parse_query",
				decls.NewInstanceOverload(
					"string_parse_query",
					[]*expr.Type{decls.String},
					decls.NewMapType(decls.String, decls.NewListType(decls.String)),
				),
			),
			decls.NewFunction("format_query",
				decls.NewInstanceOverload(
					"map_format_query",
					[]*expr.Type{decls.NewMapType(decls.String, decls.NewListType(decls.String))},
					decls.String,
				),
			),
		),
	}
}

func (l httpLib) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{
		cel.Functions(
			&functions.Overload{
				Operator: "head_string",
				Unary:    l.doHead,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "get_string",
				Unary:    l.doGet,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "get_request_string",
				Unary:    newGetRequest,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "post_string_string_bytes",
				Function: l.doPost,
			},
			&functions.Overload{
				Operator: "post_string_string_string",
				Function: l.doPost,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "post_request_string_string_bytes",
				Function: newPostRequest,
			},
			&functions.Overload{
				Operator: "post_request_string_string_string",
				Function: newPostRequest,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "request_string_string",
				Binary:   newRequest,
			},
			&functions.Overload{
				Operator: "request_string_string_bytes",
				Function: newRequestBody,
			},
			&functions.Overload{
				Operator: "request_string_string_string",
				Function: newRequestBody,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "map_basic_authentication_string_string",
				Function: l.basicAuthentication,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "map_do_request",
				Unary:    l.doRequest,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_parse_url",
				Unary:    parseURL,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "map_format_url",
				Unary:    formatURL,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "string_parse_query",
				Unary:    parseQuery,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "map_format_query",
				Unary:    formatQuery,
			},
		),
	}
}

func (l httpLib) doHead(arg ref.Val) ref.Val {
	url, ok := arg.(types.String)
	if !ok {
		return types.ValOrErr(url, "no such overload for head")
	}
	err := l.limit.Wait(context.TODO())
	if err != nil {
		return types.NewErr("%s", err)
	}
	resp, err := l.head(url)
	if err != nil {
		return types.NewErr("%s", err)
	}
	rm, err := respToMap(resp)
	if err != nil {
		return types.NewErr("%s", err)
	}
	return types.DefaultTypeAdapter.NativeToValue(rm)
}

func (l httpLib) head(url types.String) (*http.Response, error) {
	req, err := http.NewRequestWithContext(l.ctx, http.MethodHead, string(url), nil)
	if err != nil {
		return nil, err
	}
	if l.auth != nil {
		req.SetBasicAuth(l.auth.Username, l.auth.Password)
	}
	return l.client.Do(req)
}

func (l httpLib) doGet(arg ref.Val) ref.Val {
	url, ok := arg.(types.String)
	if !ok {
		return types.ValOrErr(url, "no such overload for get")
	}
	err := l.limit.Wait(context.TODO())
	if err != nil {
		return types.NewErr("%s", err)
	}
	resp, err := l.get(url)
	if err != nil {
		return types.NewErr("%s", err)
	}
	rm, err := respToMap(resp)
	if err != nil {
		return types.NewErr("%s", err)
	}
	return types.DefaultTypeAdapter.NativeToValue(rm)
}

func (l httpLib) get(url types.String) (*http.Response, error) {
	req, err := http.NewRequestWithContext(l.ctx, http.MethodGet, string(url), nil)
	if err != nil {
		return nil, err
	}
	if l.auth != nil {
		req.SetBasicAuth(l.auth.Username, l.auth.Password)
	}
	return l.client.Do(req)
}

func newGetRequest(url ref.Val) ref.Val {
	return newRequestBody(types.String("GET"), url)
}

func (l httpLib) doPost(args ...ref.Val) ref.Val {
	if len(args) != 3 {
		return types.NewErr("no such overload for post")
	}
	url, ok := args[0].(types.String)
	if !ok {
		return types.ValOrErr(url, "no such overload for request")
	}
	content, ok := args[1].(types.String)
	if !ok {
		return types.ValOrErr(content, "no such overload for request")
	}
	var body io.Reader
	switch text := args[2].(type) {
	case types.Bytes:
		if len(text) != 0 {
			body = bytes.NewReader(text)
		}
	case types.String:
		if text != "" {
			body = strings.NewReader(string(text))
		}
	default:
		return types.NewErr("invalid type for post body: %s", text.Type())
	}
	err := l.limit.Wait(context.TODO())
	if err != nil {
		return types.NewErr("%s", err)
	}
	resp, err := l.post(url, content, body)
	if err != nil {
		return types.NewErr("%s", err)
	}
	rm, err := respToMap(resp)
	if err != nil {
		return types.NewErr("%s", err)
	}
	return types.DefaultTypeAdapter.NativeToValue(rm)
}

func (l httpLib) post(url, content types.String, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(l.ctx, http.MethodPost, string(url), body)
	if err != nil {
		return nil, err
	}
	if l.auth != nil {
		req.SetBasicAuth(l.auth.Username, l.auth.Password)
	}
	req.Header.Set("Content-Type", string(content))
	return l.client.Do(req)
}

func newPostRequest(args ...ref.Val) ref.Val {
	if len(args) != 3 {
		return types.NewErr("no such overload for post request")
	}
	content, ok := args[1].(types.String)
	if !ok {
		return types.ValOrErr(content, "no such overload for request")
	}
	url := args[0]
	body := args[2]
	req, err := makeRequestBody(types.String("POST"), url, body)
	if err != nil {
		return err
	}
	h, ok := req["Header"]
	if !ok {
		h = make(http.Header)
		req["Header"] = h
	}
	h.(http.Header).Set("Content-Type", string(content))
	return types.DefaultTypeAdapter.NativeToValue(req)
}

func newRequest(method, url ref.Val) ref.Val {
	return newRequestBody(method, url)
}

func newRequestBody(args ...ref.Val) ref.Val {
	req, err := makeRequestBody(args...)
	if err != nil {
		return err
	}
	return types.DefaultTypeAdapter.NativeToValue(req)
}

func makeRequestBody(args ...ref.Val) (map[string]interface{}, ref.Val) {
	if len(args) < 2 {
		return nil, types.NewErr("no such overload for request")
	}
	method, ok := args[0].(types.String)
	if !ok {
		return nil, types.ValOrErr(method, "no such overload for request")
	}
	url, ok := args[1].(types.String)
	if !ok {
		return nil, types.ValOrErr(method, "no such overload for request")
	}
	var (
		body       ref.Val
		bodyReader io.Reader
	)
	if len(args) == 3 {
		body = args[2]
		switch body := body.(type) {
		case types.Bytes:
			if len(body) != 0 {
				bodyReader = bytes.NewReader(body)
			}
		case types.String:
			if body != "" {
				bodyReader = strings.NewReader(string(body))
			}
		default:
			return nil, types.NewErr("invalid type for request body: %s", body.Type())
		}
	}
	req, err := http.NewRequest(string(method), string(url), bodyReader)
	if err != nil {
		return nil, types.NewErr("%s", err)
	}
	reqMap, err := reqToMap(req, url, body)
	if err != nil {
		return nil, types.NewErr("%s", err)
	}
	return reqMap, nil
}

func reqToMap(req *http.Request, url, body ref.Val) (map[string]interface{}, error) {
	rm := map[string]interface{}{
		"Method":        req.Method,
		"URL":           url,
		"Proto":         req.Proto,
		"ProtoMajor":    req.ProtoMajor,
		"ProtoMinor":    req.ProtoMinor,
		"Header":        req.Header,
		"ContentLength": req.ContentLength,
		"Close":         req.Close,
		"Host":          req.Host,
	}
	if req.RequestURI != "" {
		rm["RequestURI"] = req.RequestURI
	}
	if body != nil {
		rm["Body"] = body
	}
	if req.TransferEncoding != nil {
		rm["TransferEncoding"] = req.TransferEncoding
	}
	if req.Trailer != nil {
		rm["Trailer"] = req.Trailer
	}
	if req.Response != nil {
		resp, err := respToMap(req.Response)
		if err != nil {
			return nil, err
		}
		rm["Response"] = resp
	}
	return rm, nil
}

func respToMap(resp *http.Response) (map[string]interface{}, error) {
	rm := map[string]interface{}{
		"Status":        resp.Status,
		"StatusCode":    resp.StatusCode,
		"Proto":         resp.Proto,
		"ProtoMajor":    resp.ProtoMajor,
		"ProtoMinor":    resp.ProtoMinor,
		"Header":        resp.Header,
		"ContentLength": resp.ContentLength,
		"Close":         resp.Close,
		"Uncompressed":  resp.Uncompressed,
	}
	var buf bytes.Buffer
	_, err := io.Copy(&buf, resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	rm["Body"] = buf.Bytes()
	if resp.TransferEncoding != nil {
		rm["TransferEncoding"] = resp.TransferEncoding
	}
	if resp.Trailer != nil {
		rm["Trailer"] = resp.Trailer
	}
	if resp.Request != nil {
		req, err := reqToMap(resp.Request, types.String(resp.Request.URL.String()), nil)
		if err != nil {
			return nil, err
		}
		rm["Request"] = req
	}
	return rm, nil
}

func (l httpLib) basicAuthentication(args ...ref.Val) ref.Val {
	if len(args) != 3 {
		return types.NewErr("no such overload for request")
	}
	request, ok := args[0].(traits.Mapper)
	if !ok {
		return types.ValOrErr(request, "no such overload for do_request")
	}
	username, ok := args[1].(types.String)
	if !ok {
		return types.ValOrErr(username, "no such overload for request")
	}
	password, ok := args[2].(types.String)
	if !ok {
		return types.ValOrErr(password, "no such overload for request")
	}
	reqm, err := request.ConvertToNative(reflectMapStringAnyType)
	if err != nil {
		return types.NewErr("%s", err)
	}

	// Rather than round-tripping though an http.Request, just
	// add the Authorization header into the map directly.
	// This reduces work required in the general case, and greatly
	// simplifies the case where a body has already been added
	// to the request.
	req := reqm.(map[string]interface{})
	var header http.Header
	switch h := req["Header"].(type) {
	case nil:
		header = make(http.Header)
		req["Header"] = header
	case map[string][]string:
		header = h
	case http.Header:
		header = h
	default:
		return types.NewErr("invalid type in header field: %T", h)
	}
	header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
	return types.DefaultTypeAdapter.NativeToValue(req)
}

func (l httpLib) doRequest(arg ref.Val) ref.Val {
	request, ok := arg.(traits.Mapper)
	if !ok {
		return types.ValOrErr(request, "no such overload for do_request")
	}
	reqm, err := request.ConvertToNative(reflectMapStringAnyType)
	if err != nil {
		return types.NewErr("%s", err)
	}
	req, err := mapToReq(reqm.(map[string]interface{}))
	if err != nil {
		return types.NewErr("%s", err)
	}
	// Recover the context lost during serialisation to JSON.
	req = req.WithContext(l.ctx)
	err = l.limit.Wait(l.ctx)
	if err != nil {
		return types.NewErr("%s", err)
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return types.NewErr("%s", err)
	}
	respm, err := respToMap(resp)
	if err != nil {
		return types.NewErr("%s", err)
	}
	return types.DefaultTypeAdapter.NativeToValue(respm)
}

func mapToReq(rm map[string]interface{}) (*http.Request, error) {
	if rm == nil {
		return nil, nil
	}
	req := &http.Request{}
	err := mapConv(reflect.ValueOf(req).Elem(), rm)
	return req, err
}

func mapToResp(rm map[string]interface{}) (*http.Response, error) {
	if rm == nil {
		return nil, nil
	}
	resp := &http.Response{}
	err := mapConv(reflect.ValueOf(resp).Elem(), rm)
	return resp, err
}

func mapConv(dst reflect.Value, src map[string]interface{}) error {
	rt := dst.Type()
	for i := 0; i < dst.NumField(); i++ {
		ft := rt.Field(i)
		if !ft.IsExported() {
			continue
		}
		v, ok := src[ft.Name]
		if !ok {
			continue
		}
		conv, ok := convFuncs[ft.Type.String()]
		if !ok {
			continue
		}
		val, err := conv(reflect.ValueOf(v))
		if err != nil {
			return err
		}
		dst.Field(i).Set(val)
	}
	return nil
}

var convFuncs = map[string]func(val reflect.Value) (reflect.Value, error){
	"int":                  func(val reflect.Value) (reflect.Value, error) { return val.Convert(reflectIntType), nil },
	"int64":                func(val reflect.Value) (reflect.Value, error) { return val.Convert(reflectInt64Type), nil },
	"bool":                 func(val reflect.Value) (reflect.Value, error) { return val.Convert(reflectBoolType), nil },
	"string":               func(val reflect.Value) (reflect.Value, error) { return val.Convert(reflectStringType), nil },
	"[]string":             makeStrings,
	"io.ReadCloser":        makeBody,
	"*url.URL":             makeURL,
	"http.Header":          makeMapStrings,
	"url.Values":           makeMapStrings,
	"*multipart.Form":      func(val reflect.Value) (reflect.Value, error) { panic("TODO") },
	"*tls.ConnectionState": func(val reflect.Value) (reflect.Value, error) { panic("TODO") },

	// These should pass through without this being implemented, but mark them.
	"*http.Request":  func(val reflect.Value) (reflect.Value, error) { panic("REPORT BUG: http.Request") },
	"*http.Response": func(val reflect.Value) (reflect.Value, error) { panic("REPORT BUG: http.Response") },
}

func makeMapStrings(val reflect.Value) (reflect.Value, error) {
	iface := val.Interface()
	switch iface := iface.(type) {
	case http.Header:
		return reflect.ValueOf(iface), nil
	case url.Values:
		return reflect.ValueOf(iface), nil
	case map[string][]string:
		return reflect.ValueOf(iface), nil
	case map[ref.Val]ref.Val:
		val := types.DefaultTypeAdapter.NativeToValue(iface)
		v, err := val.ConvertToNative(reflectMapStringStringSliceType)
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(v), nil
	case ref.Val:
		v, err := iface.ConvertToNative(reflectMapStringStringSliceType)
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(v.(map[string][]string)), nil
	default:
		return reflect.Value{}, fmt.Errorf("invalid type: %T", iface)
	}
}

func makeStrings(val reflect.Value) (reflect.Value, error) {
	iface := val.Interface()
	switch iface := iface.(type) {
	case []string:
		return reflect.ValueOf(iface), nil
	case []types.String:
		dst := make([]string, len(iface))
		for i, s := range iface {
			dst[i] = string(s)
		}
		return reflect.ValueOf(dst), nil
	case ref.Val:
		v, err := iface.ConvertToNative(reflectStringSliceType)
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(v), nil
	case []ref.Val:
		dst := make([]string, len(iface))
		for i, s := range iface {
			v, err := s.ConvertToNative(reflectStringType)
			if err != nil {
				return reflect.Value{}, err
			}
			dst[i] = v.(string)
		}
		return reflect.ValueOf(dst), nil
	default:
		return reflect.Value{}, fmt.Errorf("invalid type: %T", iface)
	}
}

func makeBody(val reflect.Value) (reflect.Value, error) {
	var r io.Reader
	switch val.Kind() {
	case reflect.String:
		r = strings.NewReader(val.String())
	case reflect.Slice:
		if !val.CanConvert(reflectByteSliceType) {
			return reflect.Value{}, fmt.Errorf("invalid type: %s", val.Type())
		}
		r = bytes.NewReader(val.Bytes())
	default:
		return reflect.Value{}, fmt.Errorf("invalid type: %s", val.Type())
	}
	return reflect.ValueOf(io.NopCloser(r)), nil
}

func makeURL(val reflect.Value) (reflect.Value, error) {
	if val.Kind() != reflect.String {
		return reflect.Value{}, fmt.Errorf("invalid type: %s", val.Type())
	}
	u, err := url.Parse(val.String())
	if err != nil {
		return reflect.Value{}, err
	}
	return reflect.ValueOf(u), nil
}

func parseURL(arg ref.Val) ref.Val {
	addr, ok := arg.(types.String)
	if !ok {
		return types.ValOrErr(addr, "no such overload for request")
	}
	u, err := url.Parse(string(addr))
	if err != nil {
		return types.NewErr("%s", err)
	}
	var user interface{}
	if u.User != nil {
		password, passwordSet := u.User.Password()
		user = map[string]interface{}{
			"Username":    u.User.Username(),
			"Password":    password,
			"PasswordSet": passwordSet,
		}
	}
	return types.NewStringInterfaceMap(types.DefaultTypeAdapter, map[string]interface{}{
		"Scheme":      u.Scheme,
		"Opaque":      u.Opaque,
		"User":        user,
		"Host":        u.Host,
		"Path":        u.Path,
		"RawPath":     u.RawPath,
		"ForceQuery":  u.ForceQuery,
		"RawQuery":    u.RawQuery,
		"Fragment":    u.Fragment,
		"RawFragment": u.RawFragment,
	})
}

func formatURL(arg ref.Val) ref.Val {
	urlMap, ok := arg.(traits.Mapper)
	if !ok {
		return types.ValOrErr(urlMap, "no such overload")
	}
	v, err := urlMap.ConvertToNative(reflectMapStringAnyType)
	if err != nil {
		return types.NewErr("no such overload for format_url: %v", err)
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		// This should never happen.
		return types.NewErr("unexpected type for url map: %T", v)
	}
	u := url.URL{
		Scheme:      maybeStringLookup(m, "Scheme"),
		Opaque:      maybeStringLookup(m, "Opaque"),
		Host:        maybeStringLookup(m, "Host"),
		Path:        maybeStringLookup(m, "Path"),
		RawPath:     maybeStringLookup(m, "RawPath"),
		ForceQuery:  maybeBoolLookup(m, "ForceQuery"),
		RawQuery:    maybeStringLookup(m, "RawQuery"),
		Fragment:    maybeStringLookup(m, "Fragment"),
		RawFragment: maybeStringLookup(m, "RawFragment"),
	}
	user, ok := urlMap.Find(types.String("User"))
	if ok {
		switch user := user.(type) {
		case nil:
		case traits.Mapper:
			var username types.String
			un, ok := user.Find(types.String("Username"))
			if ok {
				username, ok = un.(types.String)
				if !ok {
					return types.NewErr("invalid type for username: %s", un.Type())
				}
			}
			if user.Get(types.String("PasswordSet")) == types.True {
				var password types.String
				pw, ok := user.Find(types.String("Password"))
				if ok {
					password, ok = pw.(types.String)
					if !ok {
						return types.NewErr("invalid type for password: %s", pw.Type())
					}
				}
				u.User = url.UserPassword(string(username), string(password))
			} else {
				u.User = url.User(string(username))
			}
		default:
			if user != types.NullValue {
				return types.NewErr("unsupported type: %T", user)
			}
		}
	}
	return types.String(u.String())
}

// maybeStringLookup returns a string from m[key] if it is present and the
// empty string if not. It panics is m[key] is not a string.
func maybeStringLookup(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	return v.(string)
}

// maybeBoolLookup returns a bool from m[key] if it is present and false if
// not. It panics is m[key] is not a bool.
func maybeBoolLookup(m map[string]interface{}, key string) bool {
	v, ok := m[key]
	if !ok {
		return false
	}
	return v.(bool)
}

func parseQuery(arg ref.Val) ref.Val {
	query, ok := arg.(types.String)
	if !ok {
		return types.ValOrErr(query, "no such overload")
	}
	q, err := url.ParseQuery(string(query))
	if err != nil {
		return types.NewErr("%s", err)
	}
	return types.DefaultTypeAdapter.NativeToValue(q)
}

func formatQuery(arg ref.Val) ref.Val {
	queryMap, ok := arg.(traits.Mapper)
	if !ok {
		return types.ValOrErr(queryMap, "no such overload")
	}
	q, err := queryMap.ConvertToNative(reflectMapStringStringSliceType)
	if err != nil {
		return types.NewErr("no such overload for format_url: %v", err)
	}
	switch q := q.(type) {
	case url.Values:
		return types.String(url.Values(q).Encode())
	case map[string][]string:
		return types.String(url.Values(q).Encode())
	default:
		return types.NewErr("invalid type for format_url: %T", q)
	}
}

type digestResponse struct {
	response     string
	username     string
	usernameStar string
	realm        string
	domain       string
	uri          string
	qop          string
	nonce        string
	cnonce       string
	nc           int
	opaque       string
	algorithm    string
	userhash     bool
}

// newDigestResponse returns a new Digest Authentication response for the
// server response, given username, password, client nonce, request body
// and nonce use count for the client nonce.
func newDigestResponse(resp *http.Response, username, password, cnonce, body string, nc int) (*digestResponse, error) {
	if resp.StatusCode != http.StatusUnauthorized {
		return nil, fmt.Errorf("not a digest authentication server request: %s", resp.Status)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	for _, h := range resp.Header[http.CanonicalHeaderKey("WWW-Authenticate")] {
		c, err := parseDigestChallenge(h)
		if err != nil {
			return nil, err
		}
		r := digestResponse{
			username:  username,
			realm:     c.realm,
			domain:    c.domain,
			uri:       resp.Request.RequestURI,
			qop:       c.qop,
			nonce:     c.nonce,
			cnonce:    cnonce,
			nc:        nc + 1, // Advance our nonce count.
			opaque:    c.opaque,
			algorithm: c.algorithm,
			userhash:  c.userhash,
		}

		err = r.hash(c, password, resp.Request.Method, body)
		if err != nil {
			return nil, err
		}
		return &r, nil
	}
	return nil, errors.New("no WWW-Authenticate header")
}

func (r *digestResponse) hash(c *digestChallenge, password, method, body string) error {
	var h hash.Hash
	algorithm := strings.ToLower(r.algorithm)
	algorithm, _, sess := strings.Cut(algorithm, "_sess")
	switch algorithm {
	case "", "md5":
		h = md5.New()
	case "sha-256":
		h = sha256.New()
	case "sha-512-256":
		h = sha512_256{sha512.New()}
	}
	fmt.Fprintf(h, "%s:%s:%s", r.username, r.realm, password)
	ha1 := hex.EncodeToString(h.Sum(nil))
	h.Reset()
	if sess {
		fmt.Fprintf(h, "%s:%s:%s", ha1, c.nonce, r.cnonce)
		ha1 = hex.EncodeToString(h.Sum(nil))
		h.Reset()
	}
	var ha2 string
	auth, authInt := qopTypes(r.qop)
	switch {
	case body != "" && authInt:
		r.qop = "auth-int"
		fmt.Fprint(h, body)
		hBody := hex.EncodeToString(h.Sum(nil))
		h.Reset()
		fmt.Fprintf(h, "%s:%s:%s", method, r.uri, hBody)
		ha2 = hex.EncodeToString(h.Sum(nil))
		h.Reset()
	case auth:
		r.qop = "auth"
		fmt.Fprint(h, method+":"+r.uri)
		ha2 = hex.EncodeToString(h.Sum(nil))
		h.Reset()
	case !auth && !authInt:
		// Backward compatibility no-op.
	default:
		return fmt.Errorf("no accepted qop from %q", r.qop)
	}
	fmt.Fprintf(h, "%s:%s:%08x:%s:%s:%s", ha1, r.nonce, r.nc, r.cnonce, r.qop, ha2)
	r.response = hex.EncodeToString(h.Sum(nil))
	if r.userhash {
		h.Reset()
		fmt.Fprint(h, r.username+":"+r.realm)
		r.username = hex.EncodeToString(h.Sum(nil))
	} else {
		username, encoded := percentEncode(r.username, c.charset)
		if encoded {
			r.username = ""
			r.usernameStar = username
		}
	}
	return nil
}

// sha512_256 implements the SHA-512-256 hashing used in RFC7616 which
// differs from the Sum512/256 digest implemented in crypto/sha512.
type sha512_256 struct {
	hash.Hash
}

func (h sha512_256) Sum(b []byte) []byte {
	b = h.Hash.Sum(b)
	return b[:sha512.Size256]
}

// qopTypes returns the qop options in qop.
func qopTypes(qop string) (auth, authInt bool) {
	for _, q := range strings.Split(qop, ",") {
		switch strings.TrimSpace(q) {
		case "auth":
			auth = true
		case "auth-int":
			authInt = true
		}
	}
	return auth, authInt
}

// percentEncode encodes strings according to RFC2045 and RFC2231 for the MIME ABNF
// for parameters.
func percentEncode(s, charset string) (string, bool) {
	needEncoding := false
	for _, b := range []byte(s) {
		if needsPercentEncoding[b] {
			needEncoding = true
		}
	}
	if !needEncoding {
		return s, false
	}
	var buf strings.Builder
	buf.WriteString(charset)
	buf.WriteString("''")
	for _, b := range []byte(s) {
		if needsPercentEncoding[b] {
			fmt.Fprintf(&buf, "%%%2X", b)
			continue
		}
		buf.WriteByte(b)
	}
	return buf.String(), true
}

// needsPercentEncoding is the set of characters that need percent-encoding
// for RFC2045 and RFC2231.
var needsPercentEncoding = [256]bool{
	' ': true, '*': true, '\'': true, '%': true, '(': true, ')': true, '<': true, '>': true,
	'@': true, ',': true, ';': true, ':': true, '\\': true, '"': true, '/': true, '[': true,
	']': true, '?': true, '=': true,
}

func init() {
	for i := 0; i < '!'; i++ {
		needsPercentEncoding[i] = true
	}
	for i := 128; i < 256; i++ {
		needsPercentEncoding[i] = true
	}
}

func (r *digestResponse) String() string {
	var buf strings.Builder
	buf.WriteString("Digest")
	fmt.Fprintf(&buf, " response=%q,opaque=%q", r.response, r.opaque)
	if r.algorithm != "" {
		fmt.Fprintf(&buf, ",algorithm=%s", r.algorithm)
	}
	if r.username != "" {
		fmt.Fprintf(&buf, ",username=%q", r.username)
	}
	if r.usernameStar != "" {
		fmt.Fprintf(&buf, ",username*=%s", r.usernameStar)
	}
	if r.realm != "" {
		fmt.Fprintf(&buf, ",realm=%q", r.realm)
	}
	if r.domain != "" {
		fmt.Fprintf(&buf, ",domain=%q", r.domain)
	}
	if r.uri != "" {
		fmt.Fprintf(&buf, ",uri=%q", r.uri)
	}
	if r.nonce != "" {
		fmt.Fprintf(&buf, ",nonce=%q", r.nonce)
	}
	// RFC2617 and RFC7616 appear to disagree with how to handle
	// qop, cnonce and nc. Use the more relaxed case with the
	// expectation that the server will reject this if it's wrong.
	if r.qop != "" {
		fmt.Fprintf(&buf, ",qop=%s", r.qop)
		fmt.Fprintf(&buf, ",cnonce=%q", r.cnonce)
		fmt.Fprintf(&buf, ",nc=%08x", r.nc)
	}
	if r.userhash {
		fmt.Fprintf(&buf, ",userhash=%t", r.userhash)
	}
	return buf.String()
}

type digestChallenge struct {
	realm     string
	domain    string
	nonce     string
	opaque    string
	stale     bool
	algorithm string
	qop       string
	charset   string
	userhash  bool
}

func parseDigestChallenge(s string) (*digestChallenge, error) {
	_, c, ok := strings.Cut(s, "Digest ")
	if !ok {
		return nil, fmt.Errorf("%s is not a valid digest authentication challenge: no prefix", s)
	}
	c = strings.TrimSpace(c)
	var (
		dac digestChallenge
		err error

		n int
	)
	for len(c) != 0 {
		key, left, ok := strings.Cut(c, "=")
		if !ok {
			return nil, fmt.Errorf("%s is not a valid digest authentication challenge: no assignment", s)
		}
		switch key {
		case "realm":
			dac.realm, c, err = unq(left)
		case "domain":
			dac.domain, c, err = unq(left)
		case "nonce":
			dac.nonce, c, err = unq(left)
		case "opaque":
			dac.opaque, c, err = unq(left)
		case "stale":
			dac.stale = matchTrue(left)
			_, c, ok = strings.Cut(left, ",")
			if !ok && c != "" {
				return nil, fmt.Errorf("unable to parse unknown field in %s", c)
			}
		case "algorithm":
			dac.algorithm = findAlgo(left)
			if dac.algorithm != "" {
				c, err = advance(left, strings.TrimPrefix(left, dac.algorithm))
			}
		case "qop":
			dac.qop, c, err = unq(left)
		case "charset":
			dac.charset = findCharset(left)
			if dac.charset == "" {
				return nil, fmt.Errorf("%s is not a valid digest authentication challenge: invalid charset", s)
			}
			c, err = advance(left, strings.TrimPrefix(left, dac.charset))
		case "userhash":
			userhash := findBool(left)
			if userhash == "" {
				return nil, fmt.Errorf("%s is not a valid digest authentication challenge: invalid userhash", s)
			}
			dac.userhash = matchTrue(left)
			c, err = advance(left, strings.TrimPrefix(left, userhash))
		default:
			// Ignore unknown directives.
			left = strings.TrimSpace(left)
			if left == "" {
				break
			}
			_, c, ok = strings.Cut(left, ",")
			if !ok && c != "" {
				return nil, fmt.Errorf("unable to parse unknown field in %s", c)
			}
		}
		if err != nil {
			return nil, err
		}
		n++
	}
	if n == 0 || dac.realm == "" || dac.nonce == "" {
		return nil, fmt.Errorf("%s is not a valid digest authentication challenge", s)
	}
	return &dac, nil
}

var (
	matchTrue   = regexp.MustCompile(`^\s*\b(?i:true)\b`).MatchString
	findBool    = regexp.MustCompile(`^\s*\b(?i:true|false)\b`).FindString
	findAlgo    = regexp.MustCompile(`^\s*\b((?:MD5|SHA-256|SHA-512-256)(?:-sess)?)\b`).FindString
	findCharset = regexp.MustCompile(`^\s*\b(?:UTF-8)\b`).FindString
)

func unq(s string) (val, left string, err error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || !strings.HasPrefix(s, `"`) {
		return "", s, fmt.Errorf("%s is not a quoted string", s)
	}
	val, left, ok := strings.Cut(s[1:], `"`)
	if !ok {
		return "", s, fmt.Errorf("%s is not a quoted string", s)
	}
	left, err = advance(s, left)
	return val, left, err
}

func advance(orig, left string) (string, error) {
	left = strings.TrimSpace(left)
	if left == "" {
		return "", nil
	}
	if !strings.HasPrefix(left, ",") {
		return orig, fmt.Errorf("unexpected remainder in %q: %q", orig, left)
	}
	return strings.TrimSpace(left[1:]), nil
}
