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

package mito

import (
	"flag"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/elastic/mito/lib"
)

var update = flag.Bool("update", false, "update testscript output files")

func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"mito": Main,
	}))
}

func TestScripts(t *testing.T) {
	t.Parallel()

	p := testscript.Params{
		Dir:           filepath.Join("testdata"),
		UpdateScripts: *update,
	}
	testscript.Run(t, p)
}

func TestSend(t *testing.T) {
	chans := map[string]chan interface{}{"ch": make(chan interface{})}
	send := lib.Send(chans)

	var got interface{}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		got = <-chans["ch"]
	}()

	res, err := eval(`42.send_to("ch").close("ch")`, "", nil, send)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if res != "true" {
		t.Errorf("unexpected false result")
	}
	wg.Wait()
	if got != int64(42) {
		t.Errorf("unexpected sent result: got:%v want:42", got)
	}
}
