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
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRespToMapRedirect(t *testing.T) {
	download := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, `{"spdxVersion":"SPDX-2.3"}`)
	}))
	defer download.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", download.URL+"/file")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Length", "2")
		w.WriteHeader(http.StatusFound)
		fmt.Fprint(w, "{}")
	}))
	defer api.Close()

	resp, err := http.Get(api.URL + "/fetch-report/x")
	if err != nil {
		t.Fatalf("unexpected error from request: %v", err)
	}
	got, err := respToMap(resp, resp.Body, 1<<20)
	if err != nil {
		t.Fatalf("respToMap returned error: %v", err)
	}

	body, _ := got["Body"].([]byte)
	if string(body) != `{"spdxVersion":"SPDX-2.3"}` {
		t.Errorf("unexpected final body: %q", body)
	}
	req, _ := got["Request"].(map[string]interface{})
	hop, _ := req["Response"].(map[string]interface{})
	if hop == nil {
		t.Fatal("missing redirect response in request chain")
	}
	if code := hop["StatusCode"]; code != http.StatusFound {
		t.Errorf("unexpected redirect status code: got:%v want:%d", code, http.StatusFound)
	}
	if b, _ := hop["Body"].([]byte); len(b) != 0 {
		t.Errorf("unexpected redirect body: got:%q want empty", b)
	}
	hdr, _ := hop["Header"].(http.Header)
	if loc := hdr.Get("Location"); loc != download.URL+"/file" {
		t.Errorf("unexpected redirect Location: got:%q want:%q", loc, download.URL+"/file")
	}
}
